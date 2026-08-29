package secretsutils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetSecretValues(ctx context.Context, c client.Client, namespace string, name string, keys []string) (map[string]string, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, secret)

	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %w", namespace, name, err)
	}

	result := make(map[string]string)

	for _, key := range keys {
		value, ok := secret.Data[key]
		if !ok {
			return nil, fmt.Errorf("key %s not found in secret %s/%s", key, namespace, name)
		}

		result[key] = string(value)
	}

	return result, nil
}
