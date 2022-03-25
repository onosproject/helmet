// SPDX-FileCopyrightText: 2020-present Open Networking Foundation <info@opennetworking.org>
//
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"testing"
	"time"

	"github.com/onosproject/helmit/pkg/kubernetes"

	"github.com/onosproject/helmit/pkg/helm"
	"github.com/onosproject/helmit/pkg/test"
	"github.com/stretchr/testify/assert"
)

// ChartTestSuite is a test for chart deployment
type ChartTestSuite struct {
	test.Suite
}

// TestLocalInstall tests a local chart installation
func (s *ChartTestSuite) TestLocalInstall(t *testing.T) {
	atomix := helm.Chart("kubernetes-controller").
		Release("atomix-controller").
		Set("scope", "Namespace")
	err := atomix.Install(true)
	assert.NoError(t, err)

	raft := helm.Chart("raft-storage-controller").
		Release("raft-storage-controller").
		Set("scope", "Namespace")

	err = raft.Install(true)
	assert.NoError(t, err)

	topo := helm.Chart("onos-topo").
		Release("onos-topo").
		Set("store.controller", "atomix-controller-kubernetes-controller:5679")
	err = topo.Install(true)
	assert.NoError(t, err)

	client := kubernetes.NewForReleaseOrDie(topo)

	pods, err := client.CoreV1().Pods().List(context.Background())
	assert.NoError(t, err)
	assert.Len(t, pods, 2)

	deployment, err := client.AppsV1().
		Deployments().
		Get(context.Background(), "onos-topo")
	assert.NoError(t, err)

	pods, err = deployment.Pods().List(context.Background())
	assert.NoError(t, err)
	assert.Len(t, pods, 1)
	pod := pods[0]
	err = pod.Delete(context.Background())
	assert.NoError(t, err)

	err = deployment.Wait(context.Background(), 1*time.Minute)
	assert.NoError(t, err)

	pods, err = deployment.Pods().List(context.Background())
	assert.NoError(t, err)
	assert.Len(t, pods, 1)
	assert.NotEqual(t, pod.Name, pods[0].Name)

	services, err := client.CoreV1().Services().List(context.Background())
	assert.NoError(t, err)
	assert.Len(t, services, 2)

	err = atomix.Uninstall()
	assert.NoError(t, err)

	err = raft.Uninstall()
	assert.NoError(t, err)

	err = topo.Uninstall()
	assert.NoError(t, err)
}

// TestRemoteInstall tests a remote chart installation
func (s *ChartTestSuite) TestRemoteInstall(t *testing.T) {
	kafka := helm.Chart("kafka", "http://storage.googleapis.com/kubernetes-charts-incubator").
		Release("kafka").
		Set("replicas", 1).
		Set("zookeeper.replicaCount", 1)
	err := kafka.Install(true)
	assert.NoError(t, err)

	err = kafka.Uninstall()
	assert.NoError(t, err)
}
