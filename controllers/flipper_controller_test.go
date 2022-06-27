package controllers

import (
	"fmt"
	"time"

	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	flipperv1alpha1 "github.com/rajendragosavi/flipper-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func int32Ptr(i int32) *int32 { return &i }

var _ = Describe("Flipper controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		FlipperObjectName       = "test-flipper"
		FlipperObjectNamespace  = "default"
		FlipperObjectAPIVersion = "flipper.dev.io/v1alpha1"
		FlipperObjectKind       = "Flipper"
		TestDeploymentNamespace = "mesh"
		TestDeploymentName      = "mesh-deployment"

		timeout         = time.Second * 10
		duration        = time.Second * 10
		interval        = time.Millisecond * 250
		requeueInterval = time.Minute * 5
	)

	Context("When updating Flipper Object Status", func() {
		It("Should update Flipper Status. count when a matching Deployment gets rolled out", func() {
			By("By creating a new Flipper Object")
			ctx := context.Background()
			flipperObject := &flipperv1alpha1.Flipper{
				TypeMeta: metav1.TypeMeta{
					APIVersion: FlipperObjectAPIVersion,
					Kind:       FlipperObjectKind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      FlipperObjectName,
					Namespace: FlipperObjectNamespace,
				},
				Spec: flipperv1alpha1.FlipperSpec{
					Interval: "3m",
					Match: flipperv1alpha1.MatchRef{
						Namespace: TestDeploymentNamespace,
						Labels: map[string]string{
							"mesh": "true",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, flipperObject)).Should(Succeed())

			flipperObjectLookupKey := types.NamespacedName{Name: FlipperObjectName, Namespace: FlipperObjectNamespace}
			createdFlipperObject := &flipperv1alpha1.Flipper{}

			// We'll need to retry getting this newly created Flipper Object, given that creation may not immediately happen.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, flipperObjectLookupKey, createdFlipperObject)
				if err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			Expect(createdFlipperObject.Spec.Interval).Should(Equal("3m"))

			By("By checking the Flipper has zero deployment objects under status")
			Consistently(func() (int, error) {
				err := k8sClient.Get(ctx, flipperObjectLookupKey, createdFlipperObject)
				if err != nil {
					return -1, err
				}
				return len(createdFlipperObject.Status.Deployments), nil
			}, duration, interval).Should(Equal(0))

			By("By creating Matching deployment object in flipper.spec.match.namespace ")

			testNamespace := &apiv1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Namespace",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: TestDeploymentNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace)).Should(Succeed())

			testDeploymentObject := &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: TestDeploymentNamespace,
					Name:      TestDeploymentName,
					Labels: map[string]string{
						"mesh": "true",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mesh": "true",
						},
					},
					Template: apiv1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"mesh": "true",
							},
						},
						Spec: apiv1.PodSpec{
							Containers: []apiv1.Container{
								{
									Name:  "nginx",
									Image: "nginx:1.14.2",
									Ports: []apiv1.ContainerPort{
										{
											Name:          "nginx",
											ContainerPort: 80,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, testDeploymentObject)).Should(Succeed())

			By("By checking if deployment object is created successfully.")

			testDeploymentLookupKey := types.NamespacedName{Name: TestDeploymentName, Namespace: TestDeploymentNamespace}
			createdDeploymentObject := &appsv1.Deployment{}

			Consistently(func() (string, error) {
				err := k8sClient.Get(ctx, testDeploymentLookupKey, createdDeploymentObject)
				if err != nil {
					return "", err
				}
				return createdDeploymentObject.Name, nil
			}, duration, interval).Should(Equal(TestDeploymentName))

			By("By checking that the Flipper object has deployment object under status field after rolling out.")

			fmt.Printf("Flipper Object - %+v \n", createdFlipperObject)

			time.Sleep(time.Second * 5)
			Eventually(func() (int, error) {
				err := k8sClient.Get(ctx, flipperObjectLookupKey, createdFlipperObject)
				fmt.Println("Calling....", time.Now())
				if err != nil {
					return -1, err
				}
				return len(createdFlipperObject.Status.Deployments), nil
			}).WithTimeout(time.Minute * 3).WithPolling(time.Second * 10).Should(Equal(1))

		})
	})

})
