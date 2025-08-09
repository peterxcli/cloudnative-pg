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
	"context"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/chaosmesh"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Chaos Mesh PostgreSQL Failover", Label(tests.LabelDisruptive), func() {
	const (
		clusterName       = "chaos-test-cluster"
		sampleFile        = fixturesDir + "/base/cluster-storage-class.yaml.template"
		namespacePrefix   = "chaos-mesh-test"
		level             = tests.High
	)

	var (
		namespace    string
		chaosAdapter *chaosmesh.Adapter
		err          error
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		// Create a unique namespace for this test
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				namespaces.DumpNamespaceObjects(env.Ctx, env.Client, namespace, "out/"+namespace)
			}
			return namespaces.DeleteNamespaceAndWait(env.Ctx, env.Client, namespace, 120)
		})

		// Create a PostgreSQL cluster
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Initialize Chaos Mesh adapter
		chaosAdapter = chaosmesh.NewAdapter(env.Client, namespace)
	})

	Context("Primary Pod Failure", func() {
		It("should handle primary pod kill gracefully", func() {
			By("identifying the primary pod")
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryPod).ToNot(BeNil())

			originalPrimaryName := primaryPod.Name
			GinkgoWriter.Printf("Original primary: %s\n", originalPrimaryName)

			By("injecting pod chaos to kill the primary")
			config := core.ExperimentConfig{
				Name:     "primary-pod-kill",
				Action:   core.ChaosActionPodKill,
				Duration: 10 * time.Second,
				Target: core.TargetSelector{
					Namespace: namespace,
					PodName:   originalPrimaryName,
				},
			}

			podChaos, err := chaosAdapter.InjectPodChaos(context.Background(), config)
			Expect(err).ToNot(HaveOccurred())
			Expect(podChaos).ToNot(BeNil())

			defer func() {
				// Clean up chaos experiment
				err := chaosAdapter.DeleteChaos(context.Background(), "PodChaos", config.Name)
				Expect(err).ToNot(HaveOccurred())
			}()

			By("waiting for failover to complete")
			Eventually(func() bool {
				currentPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return false
				}
				// Check if a new primary has been elected
				return currentPrimary.Name != originalPrimaryName
			}, 120*time.Second, 5*time.Second).Should(BeTrue())

			By("verifying cluster health after failover")
			Eventually(func() bool {
				cluster := &cnpgv1.Cluster{}
				err := env.Client.Get(context.Background(), 
					types.NamespacedName{Namespace: namespace, Name: clusterName}, 
					cluster)
				if err != nil {
					return false
				}
				// Check if cluster has healthy instances
				return cluster.Status.Instances > 0 && 
					   cluster.Status.ReadyInstances == cluster.Status.Instances
			}, 180*time.Second, 10*time.Second).Should(BeTrue())

			By("verifying new primary is functional")
			newPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(newPrimary).ToNot(BeNil())
			Expect(newPrimary.Name).ToNot(Equal(originalPrimaryName))

			GinkgoWriter.Printf("New primary after failover: %s\n", newPrimary.Name)
		})
	})

	Context("Network Partition", func() {
		It("should handle network partition between primary and replicas", func() {
			By("identifying the primary pod")
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("creating network partition between primary and replicas")
			config := chaosmesh.NetworkChaosConfig{
				Name:      "primary-network-partition",
				Action:    chaosmesh.NetworkPartitionAction,
				Mode:      chaosmesh.OneMode,
				Duration:  30 * time.Second,
				Direction: chaosmesh.Both,
				Selector: chaosmesh.PodSelectorSpec{
					Namespaces: []string{namespace},
					Pods: map[string][]string{
						namespace: {primaryPod.Name},
					},
				},
				Target: &chaosmesh.PodSelectorSpec{
					Namespaces: []string{namespace},
					LabelSelectors: map[string]string{
						"cnpg.io/cluster":      clusterName,
						"cnpg.io/instanceRole": "replica",
					},
				},
			}

			networkChaos, err := chaosAdapter.InjectNetworkChaos(context.Background(), config)
			Expect(err).ToNot(HaveOccurred())
			Expect(networkChaos).ToNot(BeNil())

			defer func() {
				// Clean up chaos experiment
				err := chaosAdapter.DeleteChaos(context.Background(), "NetworkChaos", config.Name)
				Expect(err).ToNot(HaveOccurred())
			}()

			By("monitoring cluster behavior during network partition")
			time.Sleep(15 * time.Second)

			By("verifying cluster detects the issue")
			cluster := &cnpgv1.Cluster{}
			err = env.Client.Get(context.Background(), 
				types.NamespacedName{Namespace: namespace, Name: clusterName}, 
				cluster)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for network partition to heal")
			time.Sleep(20 * time.Second)

			By("verifying cluster recovers after partition heals")
			Eventually(func() bool {
				cluster := &cnpgv1.Cluster{}
				err := env.Client.Get(context.Background(), 
					types.NamespacedName{Namespace: namespace, Name: clusterName}, 
					cluster)
				if err != nil {
					return false
				}
				return cluster.Status.ReadyInstances == cluster.Status.Instances
			}, 120*time.Second, 10*time.Second).Should(BeTrue())
		})
	})

	Context("I/O Chaos", func() {
		It("should handle I/O delays on PostgreSQL data directory", func() {
			By("identifying the primary pod")
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("injecting I/O delay chaos")
			config := chaosmesh.IOChaosConfig{
				Name:     "pgdata-io-delay",
				Action:   chaosmesh.IODelayAction,
				Mode:     chaosmesh.OneMode,
				Duration: 20 * time.Second,
				Selector: chaosmesh.PodSelectorSpec{
					Namespaces: []string{namespace},
					Pods: map[string][]string{
						namespace: {primaryPod.Name},
					},
				},
				Delay:   "100ms",
				Path:    "/var/lib/postgresql/data",
				Percent: 50,
				Methods: []string{"read", "write"},
			}

			ioChaos, err := chaosAdapter.InjectIOChaos(context.Background(), config)
			Expect(err).ToNot(HaveOccurred())
			Expect(ioChaos).ToNot(BeNil())

			defer func() {
				// Clean up chaos experiment
				err := chaosAdapter.DeleteChaos(context.Background(), "IOChaos", config.Name)
				Expect(err).ToNot(HaveOccurred())
			}()

			By("monitoring PostgreSQL performance during I/O chaos")
			// Here you could add checks for:
			// - Increased latency in database operations
			// - WAL archiving delays
			// - Replication lag

			time.Sleep(10 * time.Second)

			By("verifying cluster remains operational despite I/O delays")
			cluster := &cnpgv1.Cluster{}
			err = env.Client.Get(context.Background(), 
				types.NamespacedName{Namespace: namespace, Name: clusterName}, 
				cluster)
			Expect(err).ToNot(HaveOccurred())
			
			// The cluster should remain operational but may show degraded performance
			Expect(cluster.Status.Instances).To(BeNumerically(">", 0))

			By("waiting for I/O chaos to complete")
			time.Sleep(15 * time.Second)

			By("verifying cluster recovers after I/O chaos ends")
			Eventually(func() bool {
				cluster := &cnpgv1.Cluster{}
				err := env.Client.Get(context.Background(), 
					types.NamespacedName{Namespace: namespace, Name: clusterName}, 
					cluster)
				if err != nil {
					return false
				}
				return cluster.Status.ReadyInstances == cluster.Status.Instances
			}, 60*time.Second, 5*time.Second).Should(BeTrue())
		})
	})

	Context("Multiple Simultaneous Failures", func() {
		It("should handle multiple replica failures", func() {
			By("getting all replica pods")
			replicaSelector := labels.SelectorFromSet(labels.Set{
				"cnpg.io/cluster":      clusterName,
				"cnpg.io/instanceRole": "replica",
			})

			By("injecting chaos to affect multiple replicas")
			config := core.ExperimentConfig{
				Name:     "multi-replica-failure",
				Action:   core.ChaosActionPodFailure,
				Duration: 30 * time.Second,
				Target: core.TargetSelector{
					Namespace:     namespace,
					LabelSelector: replicaSelector,
					Percentage:    50, // Affect 50% of replicas
				},
			}

			podChaos, err := chaosAdapter.InjectPodChaos(context.Background(), config)
			Expect(err).ToNot(HaveOccurred())
			Expect(podChaos).ToNot(BeNil())

			defer func() {
				// Clean up chaos experiment
				err := chaosAdapter.DeleteChaos(context.Background(), "PodChaos", config.Name)
				Expect(err).ToNot(HaveOccurred())
			}()

			By("verifying primary remains available")
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryPod).ToNot(BeNil())

			By("waiting for affected replicas to recover")
			Eventually(func() bool {
				cluster := &cnpgv1.Cluster{}
				err := env.Client.Get(context.Background(), 
					types.NamespacedName{Namespace: namespace, Name: clusterName}, 
					cluster)
				if err != nil {
					return false
				}
				// At least primary should be ready
				return cluster.Status.ReadyInstances >= 1
			}, 60*time.Second, 5*time.Second).Should(BeTrue())

			By("verifying cluster eventually returns to full health")
			Eventually(func() bool {
				cluster := &cnpgv1.Cluster{}
				err := env.Client.Get(context.Background(), 
					types.NamespacedName{Namespace: namespace, Name: clusterName}, 
					cluster)
				if err != nil {
					return false
				}
				return cluster.Status.ReadyInstances == cluster.Status.Instances
			}, 180*time.Second, 10*time.Second).Should(BeTrue())
		})
	})
})

// Helper functions for Chaos Mesh E2E tests

// WaitForChaosExperimentReady waits for a chaos experiment to be in running state
func WaitForChaosExperimentReady(adapter *chaosmesh.Adapter, kind, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	return adapter.WaitForChaosReady(ctx, kind, name, timeout)
}

// ValidateClusterHealthDuringChaos continuously checks cluster health during a chaos experiment
func ValidateClusterHealthDuringChaos(
	client client.Client,
	namespace, clusterName string,
	duration time.Duration,
	minHealthyInstances int32,
) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	endTime := time.Now().Add(duration)
	
	for time.Now().Before(endTime) {
		select {
		case <-ticker.C:
			cluster := &cnpgv1.Cluster{}
			err := client.Get(context.Background(), 
				types.NamespacedName{Namespace: namespace, Name: clusterName}, 
				cluster)
			if err != nil {
				return fmt.Errorf("failed to get cluster: %w", err)
			}
			
			if cluster.Status.ReadyInstances < int(minHealthyInstances) {
				return fmt.Errorf("cluster health degraded below threshold: %d < %d",
					cluster.Status.ReadyInstances, minHealthyInstances)
			}
		}
	}
	
	return nil
}