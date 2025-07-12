package controller

import (
	"context"
	"encoding/json"
	"os"
	"strconv"

	cloudflarecontroller "github.com/STRRL/cloudflare-tunnel-ingress-controller/pkg/cloudflare-controller"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func CreateOrUpdateControlledCloudflared(
	ctx context.Context,
	kubeClient client.Client,
	tunnelClient cloudflarecontroller.TunnelClientInterface,
	namespace string,
	protocol string,
) error {
	logger := log.FromContext(ctx)
	list := appsv1.DeploymentList{}
	err := kubeClient.List(ctx, &list, &client.ListOptions{
		Namespace: namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"strrl.dev/cloudflare-tunnel-ingress-controller": "controlled-cloudflared-connector",
		}),
	})
	if err != nil {
		return errors.Wrapf(err, "list controlled-cloudflared-connector in namespace %s", namespace)
	}

	if len(list.Items) > 0 {
		// Check if the existing deployment needs to be updated
		existingDeployment := &list.Items[0]
		desiredReplicas, err := getDesiredReplicas()
		if err != nil {
			return errors.Wrap(err, "get desired replicas")
		}

		needsUpdate := false
		if *existingDeployment.Spec.Replicas != desiredReplicas {
			needsUpdate = true
		}

		if len(existingDeployment.Spec.Template.Spec.Containers) > 0 {
			container := &existingDeployment.Spec.Template.Spec.Containers[0]
			if container.Image != os.Getenv("CLOUDFLARED_IMAGE") {
				needsUpdate = true
			}
			if string(container.ImagePullPolicy) != os.Getenv("CLOUDFLARED_IMAGE_PULL_POLICY") {
				needsUpdate = true
			}
		}

		if needsUpdate {
			token, err := tunnelClient.FetchTunnelToken(ctx)
			if err != nil {
				return errors.Wrap(err, "fetch tunnel token")
			}

			updatedDeployment := cloudflaredConnectDeploymentTemplating(protocol, token, namespace, desiredReplicas)
			existingDeployment.Spec = updatedDeployment.Spec
			err = kubeClient.Update(ctx, existingDeployment)
			if err != nil {
				return errors.Wrap(err, "update controlled-cloudflared-connector deployment")
			}
			logger.Info("Updated controlled-cloudflared-connector deployment", "namespace", namespace)
		}

		return nil
	}

	token, err := tunnelClient.FetchTunnelToken(ctx)
	if err != nil {
		return errors.Wrap(err, "fetch tunnel token")
	}

	replicas, err := getDesiredReplicas()
	if err != nil {
		return errors.Wrap(err, "get desired replicas")
	}

	deployment := cloudflaredConnectDeploymentTemplating(protocol, token, namespace, replicas)
	err = kubeClient.Create(ctx, deployment)
	if err != nil {
		return errors.Wrap(err, "create controlled-cloudflared-connector deployment")
	}
	logger.Info("Created controlled-cloudflared-connector deployment", "namespace", namespace)
	return nil
}

func cloudflaredConnectDeploymentTemplating(protocol string, token string, namespace string, replicas int32) *appsv1.Deployment {
	appName := "controlled-cloudflared-connector"

	// Use default values if environment variables are empty
	image := os.Getenv("CLOUDFLARED_IMAGE")
	if image == "" {
		image = "cloudflare/cloudflared:latest"
	}

	pullPolicy := os.Getenv("CLOUDFLARED_IMAGE_PULL_POLICY")
	if pullPolicy == "" {
		pullPolicy = "IfNotPresent"
	}

	// Get scheduling configuration from environment variables
	nodeSelector := getNodeSelectorFromEnv()
	tolerations := getTolerationsFromEnv()
	affinity := getAffinityFromEnv()

	podSpec := v1.PodSpec{
		Containers: []v1.Container{
			{
				Name:            appName,
				Image:           image,
				ImagePullPolicy: v1.PullPolicy(pullPolicy),
				Command: []string{
					"cloudflared",
					"--edge-ip-version",
					"auto",
					"--protocol",
					protocol,
					"--no-autoupdate",
					"tunnel",
					"--metrics",
					"[::]:44483",
					"run",
					"--token",
					token,
				},
			},
		},
		RestartPolicy: v1.RestartPolicyAlways,
	}

	// Apply scheduling configuration if provided
	if nodeSelector != nil {
		podSpec.NodeSelector = nodeSelector
	}
	if tolerations != nil {
		podSpec.Tolerations = tolerations
	}
	if affinity != nil {
		podSpec.Affinity = affinity
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": appName,
				"strrl.dev/cloudflare-tunnel-ingress-controller": "controlled-cloudflared-connector",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": appName,
					"strrl.dev/cloudflare-tunnel-ingress-controller": "controlled-cloudflared-connector",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: appName,
					Labels: map[string]string{
						"app": appName,
						"strrl.dev/cloudflare-tunnel-ingress-controller": "controlled-cloudflared-connector",
					},
				},
				Spec: podSpec,
			},
		},
	}
}

func getDesiredReplicas() (int32, error) {
	replicaCount := os.Getenv("CLOUDFLARED_REPLICA_COUNT")
	if replicaCount == "" {
		return 1, nil
	}
	replicas, err := strconv.ParseInt(replicaCount, 10, 32)
	if err != nil {
		return 0, errors.Wrap(err, "invalid replica count")
	}
	return int32(replicas), nil
}

// getNodeSelectorFromEnv parses the CLOUDFLARED_NODE_SELECTOR environment variable
func getNodeSelectorFromEnv() map[string]string {
	nodeSelectorJSON := os.Getenv("CLOUDFLARED_NODE_SELECTOR")
	if nodeSelectorJSON == "" || nodeSelectorJSON == "{}" {
		return nil
	}

	var nodeSelector map[string]string
	err := json.Unmarshal([]byte(nodeSelectorJSON), &nodeSelector)
	if err != nil {
		// Log error but don't fail deployment
		return nil
	}

	return nodeSelector
}

// getTolerationsFromEnv parses the CLOUDFLARED_TOLERATIONS environment variable
func getTolerationsFromEnv() []v1.Toleration {
	tolerationsJSON := os.Getenv("CLOUDFLARED_TOLERATIONS")
	if tolerationsJSON == "" || tolerationsJSON == "[]" {
		return nil
	}

	var tolerations []v1.Toleration
	err := json.Unmarshal([]byte(tolerationsJSON), &tolerations)
	if err != nil {
		// Log error but don't fail deployment
		return nil
	}

	return tolerations
}

// getAffinityFromEnv parses the CLOUDFLARED_AFFINITY environment variable
func getAffinityFromEnv() *v1.Affinity {
	affinityJSON := os.Getenv("CLOUDFLARED_AFFINITY")
	if affinityJSON == "" || affinityJSON == "{}" {
		return nil
	}

	var affinity v1.Affinity
	err := json.Unmarshal([]byte(affinityJSON), &affinity)
	if err != nil {
		// Log error but don't fail deployment
		return nil
	}

	return &affinity
}
