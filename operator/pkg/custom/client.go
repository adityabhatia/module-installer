package custom

import (
	"context"
	"github.com/kyma-project/manifest-operator/operator/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

type ClusterClient struct {
	DefaultClient client.Client
}

func (cc *ClusterClient) GetNewClient(restConfig *rest.Config) (client.Client, error) {
	cluster, err := cluster.New(restConfig)
	if err != nil {
		return nil, err
	}
	return cluster.GetClient(), nil
}

func (cc *ClusterClient) GetRestConfig(ctx context.Context, kymaOwner string, namespace string) (*rest.Config, error) {
	kubeConfigSecret := v1.Secret{}
	if err := cc.DefaultClient.Get(ctx, client.ObjectKey{Name: kymaOwner, Namespace: namespace}, &kubeConfigSecret); err != nil {
		return nil, err
	}

	kubeconfigString := string(kubeConfigSecret.Data["config"])
	restConfig, err := util.GetConfig(kubeconfigString, "")
	if err != nil {
		return nil, err
	}
	return restConfig, err
}