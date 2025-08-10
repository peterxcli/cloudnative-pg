/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/experiments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/metrics"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/safety"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
)

var _ = Describe("Chaos Testing - Primary Failure", Label(tests.LabelSelfHealing), func() {
	const (
		level       = tests.High
		clusterName = "chaos-primary-failure"
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml.template"
		tableName   = "chaos_test"
	)

	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("Primary pod failure with chaos injection", func() {
		It("should recover from primary failure and maintain data consistency", func() {
			// Create namespace for the test
			const namespacePrefix = "chaos-primary"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			By("creating a 3-instance PostgreSQL cluster", func() {
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
			})

			var initialPrimary string
			var initialPrimaryPod *corev1.Pod

			By("identifying the initial primary", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())
				initialPrimary = cluster.Status.CurrentPrimary
				Expect(initialPrimary).NotTo(BeEmpty())

				initialPrimaryPod, err = podutils.Get(env.Ctx, env.Client, namespace, initialPrimary)
				Expect(err).NotTo(HaveOccurred())
			})

			By("creating test data on primary", func() {
				query := fmt.Sprintf(`
					CREATE TABLE IF NOT EXISTS %s (
						id SERIAL PRIMARY KEY,
						data TEXT,
						created_at TIMESTAMP DEFAULT NOW()
					);
					INSERT INTO %s (data) 
					SELECT 'test_' || i 
					FROM generate_series(1, 1000) i;
				`, tableName, tableName)

				_, _, err := exec.EventuallyExecQueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   initialPrimary,
					},
					postgres.PostgresDBName,
					query,
					RetryTimeout,
					PollingTime,
				)
				Expect(err).NotTo(HaveOccurred())
			})

			var recordCount string
			By("verifying data was written", func() {
				query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
				out, _, err := exec.EventuallyExecQueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   initialPrimary,
					},
					postgres.PostgresDBName,
					query,
					RetryTimeout,
					PollingTime,
				)
				Expect(err).NotTo(HaveOccurred())
				recordCount = strings.TrimSpace(out)
				Expect(recordCount).To(Equal("1000"))
			})

			By("setting up chaos experiment configuration", func() {
				GinkgoWriter.Printf("Configuring chaos experiment for primary pod %s\n", initialPrimary)
			})

			// Configure the chaos experiment
			chaosConfig := core.ExperimentConfig{
				Name:        "primary-failure-chaos",
				Description: "Inject primary pod failure to test automatic failover",
				Target: core.TargetSelector{
					Namespace: namespace,
					LabelSelector: labels.SelectorFromSet(labels.Set{
						"cnpg.io/cluster":      clusterName,
						"cnpg.io/instanceRole": "primary",
					}),
				},
				Action:   core.ChaosActionPodKill,
				Duration: 10 * time.Second, // Short duration for testing
			}

			// Configure safety controller
			safetyConfig := safety.SafetyConfig{
				MaxFailurePercent:   50,
				MinHealthyReplicas:  2,
				MaxDataLagBytes:     10000000, // 10MB
				MaxRecoveryTime:     3 * time.Minute,
				EnableEmergencyStop: false, // Disabled for automated testing
				ClusterNamespace:    namespace,
				ClusterName:         clusterName,
			}

			By("initializing chaos safety controller", func() {
				safetyController := safety.NewController(env.Client, safetyConfig)
				err := safetyController.Start(env.Ctx)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					safetyController.Stop()
				})
			})

			// Set up metrics collection
			metricsCollector := metrics.NewClusterMetricsCollector(
				env.Client, namespace, clusterName)

			// Create the chaos experiment
			experiment := experiments.NewPodChaosExperiment(chaosConfig, env.Client)
			experiment.AddMetricsCollector(metricsCollector)

			By("executing chaos experiment", func() {
				err := experiment.Setup(env.Ctx)
				Expect(err).NotTo(HaveOccurred())

				err = experiment.Run(env.Ctx)
				Expect(err).NotTo(HaveOccurred())
			})

			var newPrimary string
			By("waiting for automatic failover", func() {
				Eventually(func() bool {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return false
					}

					// Check if failover occurred
					if cluster.Status.CurrentPrimary != initialPrimary &&
						cluster.Status.CurrentPrimary != "" {
						newPrimary = cluster.Status.CurrentPrimary
						GinkgoWriter.Printf("Failover detected: %s -> %s\n",
							initialPrimary, newPrimary)
						return true
					}
					return false
				}, 2*time.Minute, 5*time.Second).Should(BeTrue(),
					"Failover should occur within 2 minutes")
			})

			By("waiting for cluster to stabilize", func() {
				Eventually(func() bool {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return false
					}

					return cluster.Status.Phase == apiv1.PhaseHealthy &&
						cluster.Status.CurrentPrimary == cluster.Status.TargetPrimary &&
						cluster.Status.ReadyInstances == 3
				}, 3*time.Minute, 5*time.Second).Should(BeTrue(),
					"Cluster should return to healthy state")
			})

			By("verifying data consistency after failover", func() {
				query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
				out, _, err := exec.EventuallyExecQueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   newPrimary,
					},
					postgres.PostgresDBName,
					query,
					RetryTimeout,
					PollingTime,
				)
				Expect(err).NotTo(HaveOccurred())
				newCount := strings.TrimSpace(out)
				Expect(newCount).To(Equal(recordCount),
					"Data should be consistent after failover")
			})

			By("cleaning up chaos experiment", func() {
				err := experiment.Cleanup(env.Ctx)
				Expect(err).NotTo(HaveOccurred())
			})

			By("analyzing chaos metrics", func() {
				result := experiment.GetResult()
				Expect(result.Status).To(Equal(core.ExperimentStatusCompleted))
				Expect(result.SafetyAborted).To(BeFalse(),
					"Experiment should not be aborted by safety checks")

				// Check if metrics were collected
				_, hasMetrics := result.Metrics["cluster-"+clusterName+".resilience"]
				Expect(hasMetrics).To(BeTrue(), "Resilience metrics should be collected")

				// Log experiment events for debugging
				GinkgoWriter.Printf("Experiment Events:\n")
				for _, event := range result.Events {
					GinkgoWriter.Printf("  [%s] %s: %s\n",
						event.Severity, event.Type, event.Message)
				}
			})

			By("verifying old primary pod is recreated", func() {
				Eventually(func() bool {
					pod := &corev1.Pod{}
					err := env.Client.Get(env.Ctx,
						types.NamespacedName{
							Namespace: namespace,
							Name:      initialPrimary,
						}, pod)

					if err != nil {
						return false
					}

					// Check if it's a new pod (different UID)
					return pod.UID != initialPrimaryPod.UID &&
						pod.Status.Phase == corev1.PodRunning
				}, 3*time.Minute, 5*time.Second).Should(BeTrue(),
					"Old primary pod should be recreated and running")
			})

			By("verifying replication is working on all instances", func() {
				// Insert new data on the new primary
				query := fmt.Sprintf(
					"INSERT INTO %s (data) VALUES ('post_failover_test')",
					tableName)

				_, _, err := exec.EventuallyExecQueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   newPrimary,
					},
					postgres.PostgresDBName,
					query,
					RetryTimeout,
					PollingTime,
				)
				Expect(err).NotTo(HaveOccurred())

				// Verify data is replicated to all standbys
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())

				for _, pod := range podList.Items {
					if pod.Name == newPrimary {
						continue // Skip primary
					}

					Eventually(func() bool {
						query := fmt.Sprintf(
							"SELECT COUNT(*) FROM %s WHERE data = 'post_failover_test'",
							tableName)

						out, _, err := exec.QueryInInstancePod(
							env.Ctx, env.Client, env.Interface, env.RestClientConfig,
							exec.PodLocator{
								Namespace: namespace,
								PodName:   pod.Name,
							},
							postgres.PostgresDBName,
							query,
						)

						if err != nil {
							return false
						}

						count := strings.TrimSpace(out)
						return count == "1"
					}, 30*time.Second, 2*time.Second).Should(BeTrue(),
						fmt.Sprintf("Data should be replicated to %s", pod.Name))
				}
			})
		})
	})

	Context("Multiple simultaneous failures", func() {
		It("should handle multiple pod failures gracefully", func() {
			Skip("Advanced chaos scenario - to be implemented")
			// This would test more complex scenarios like:
			// - Multiple replica failures
			// - Network partitions
			// - Resource exhaustion
		})
	})
})
