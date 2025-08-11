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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/experiments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/metrics"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/safety"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
)

// This is an example showing how chaos experiments would integrate with E2E tests
var _ = Describe("Chaos Testing Example", Label(tests.LabelSelfHealing), func() {
	const (
		level       = tests.High
		clusterName = "chaos-example"
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml.template"
	)

	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("Chaos experiment integration", func() {
		It("demonstrates chaos testing with safety controls", func() {
			Skip("This is an example test showing chaos integration patterns")

			// Standard namespace creation
			const namespacePrefix = "chaos-example"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			// Note: In production tests, namespace cleanup is typically handled by the test framework

			// 1. Create cluster using standard E2E patterns
			By("creating a PostgreSQL cluster", func() {
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
			})

			// 2. Wait for cluster readiness
			By("waiting for cluster to be ready", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())

				// Wait for healthy status
				Eventually(func() bool {
					cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					return err == nil && cluster.Status.ReadyInstances == 3
				}, 180*time.Second, 5*time.Second).Should(BeTrue())
			})

			// 3. Set up chaos experiment
			By("configuring chaos experiment", func() {
				// Configure the experiment
				chaosConfig := core.ExperimentConfig{
					Name:        "pod-failure-example",
					Description: "Example pod failure experiment",
					Target: core.TargetSelector{
						Namespace: namespace,
						LabelSelector: labels.SelectorFromSet(labels.Set{
							"cnpg.io/cluster":      clusterName,
							"cnpg.io/instanceRole": "primary",
						}),
					},
					Action:   core.ChaosActionPodKill,
					Duration: 10 * time.Second,
				}

				// Set up safety
				safetyConfig := safety.SafetyConfig{
					MinHealthyReplicas:  2,
					MaxRecoveryTime:     2 * time.Minute,
					EnableEmergencyStop: false,
					ClusterNamespace:    namespace,
					ClusterName:         clusterName,
				}

				safetyController := safety.NewController(env.Client, safetyConfig)
				err := safetyController.Start(env.Ctx)
				Expect(err).NotTo(HaveOccurred())
				defer safetyController.Stop()

				// Create metrics collector
				metricsCollector := metrics.NewClusterMetricsCollector(
					env.Client, namespace, clusterName)

				// Create experiment
				experiment := experiments.NewPodChaosExperiment(chaosConfig, env.Client)
				experiment.AddMetricsCollector(metricsCollector)
				experiment.AddSafetyCheck(&safety.ClusterHealthCheck{
					Namespace:          namespace,
					ClusterName:        clusterName,
					MinHealthyReplicas: 2,
				})

				// Run experiment
				err = experiment.Setup(env.Ctx)
				Expect(err).NotTo(HaveOccurred())

				err = experiment.Run(env.Ctx)
				Expect(err).NotTo(HaveOccurred())

				// Clean up
				err = experiment.Cleanup(env.Ctx)
				Expect(err).NotTo(HaveOccurred())

				// Verify results
				result := experiment.GetResult()
				Expect(result.Status).To(Equal(core.ExperimentStatusCompleted))
				Expect(result.SafetyAborted).To(BeFalse())
			})

			// 4. Verify cluster recovery
			By("verifying cluster recovery", func() {
				Eventually(func() bool {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					if err != nil {
						return false
					}
					return cluster.Status.ReadyInstances == 3 &&
						cluster.Status.CurrentPrimary == cluster.Status.TargetPrimary
				}, 3*time.Minute, 5*time.Second).Should(BeTrue(),
					"Cluster should recover to healthy state")
			})
		})
	})
})
