/*
 * Copyright (c) 2026, salesforce.com, inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see LICENSE.txt file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package ingress

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/salesforce/sloop/pkg/sloop/kubeextractor"
	"github.com/salesforce/sloop/pkg/sloop/store/typed"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubernetesFake "k8s.io/client-go/kubernetes/fake"
)

// An Event served on /api/v1 with only the events.k8s.io/v1 field names
const eventsV1Payload = `{
  "apiVersion": "v1",
  "kind": "Event",
  "metadata": {"name": "my-app-5d8f7c6b9-x2x7q.18dcf5b287d4a557", "namespace": "default", "uid": "72d4c858-0000-0000-0000-000000000000", "resourceVersion": "5940321610", "creationTimestamp": "2026-10-09T20:14:58Z"},
  "regarding": {"kind": "Pod", "namespace": "default", "name": "my-app-5d8f7c6b9-x2x7q", "uid": "2358ba5b-0000-0000-0000-000000000000", "apiVersion": "v1", "fieldPath": "spec.containers{app}"},
  "note": "Pulling image \"busybox\"",
  "reason": "Pulling",
  "type": "Normal",
  "deprecatedSource": {"component": "kubelet", "host": "node-1"},
  "deprecatedFirstTimestamp": "2026-10-09T20:14:58Z",
  "deprecatedLastTimestamp": "2026-10-09T20:16:10Z",
  "deprecatedCount": 3,
  "reportingController": "kubelet",
  "reportingInstance": "node-1"
}`

const coreV1Payload = `{
  "apiVersion": "v1",
  "kind": "Event",
  "metadata": {"name": "my-app-5d8f7c6b9-x2x7q.18dcf5b287d4a557", "namespace": "default"},
  "involvedObject": {"kind": "Pod", "namespace": "default", "name": "my-app-5d8f7c6b9-x2x7q"},
  "message": "Pulling image \"busybox\"",
  "reason": "Pulling",
  "type": "Normal",
  "source": {"component": "kubelet", "host": "node-1"},
  "firstTimestamp": "2026-10-09T20:14:58Z",
  "lastTimestamp": "2026-10-09T20:16:10Z",
  "count": 3
}`

func toUnstructured(t *testing.T, payload string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	assert.NoError(t, obj.UnmarshalJSON([]byte(payload)))
	return obj
}

func toJson(t *testing.T, obj interface{}) string {
	bytes, err := json.Marshal(obj)
	assert.NoError(t, err)
	return string(bytes)
}

func assertSloopCanReadEvent(t *testing.T, payload string) {
	eventInfo, err := kubeextractor.ExtractEventInfo(payload)
	assert.NoError(t, err)
	assert.Equal(t, 3, eventInfo.Count)
	assert.Equal(t, time.Date(2026, 10, 9, 20, 14, 58, 0, time.UTC), eventInfo.FirstTimestamp)
	assert.Equal(t, time.Date(2026, 10, 9, 20, 16, 10, 0, time.UTC), eventInfo.LastTimestamp)
	assert.Equal(t, "Pulling", eventInfo.Reason)

	involvedObject, err := kubeextractor.ExtractInvolvedObject(payload)
	assert.NoError(t, err)
	assert.Equal(t, "Pod", involvedObject.Kind)
	assert.Equal(t, "default", involvedObject.Namespace)
	assert.Equal(t, "my-app-5d8f7c6b9-x2x7q", involvedObject.Name)
}

// Shows the bug: decoding the events.k8s.io/v1 shape into corev1.Event (what the typed informer does) drops the fields
func Test_typedEventDecodeDropsEventsV1Fields(t *testing.T) {
	event := &corev1.Event{}
	assert.NoError(t, json.Unmarshal([]byte(eventsV1Payload), event))
	payload := toJson(t, event)

	assert.Contains(t, payload, `"involvedObject":{}`)
	assert.NotContains(t, payload, "Pulling image")
	eventInfo, err := kubeextractor.ExtractEventInfo(payload)
	assert.NoError(t, err)
	assert.Equal(t, 0, eventInfo.Count)
	assert.True(t, eventInfo.LastTimestamp.IsZero())
}

func Test_normalizeEvent_EventsV1(t *testing.T) {
	event := toUnstructured(t, eventsV1Payload)
	payload := toJson(t, normalizeEvent(event))

	assertSloopCanReadEvent(t, payload)
	result := map[string]interface{}{}
	assert.NoError(t, json.Unmarshal([]byte(payload), &result))
	assert.Equal(t, "Pulling image \"busybox\"", result["message"])
	assert.Equal(t, map[string]interface{}{"component": "kubelet", "host": "node-1"}, result["source"])
	assert.Equal(t, "kubelet", result["reportingComponent"])
}

func Test_normalizeEvent_DoesNotModifyInput(t *testing.T) {
	event := toUnstructured(t, eventsV1Payload)
	before := event.DeepCopy()
	_ = normalizeEvent(event)
	assert.Equal(t, before, event)
}

func Test_normalizeEvent_CoreV1Unchanged(t *testing.T) {
	event := toUnstructured(t, coreV1Payload)
	normalized := normalizeEvent(event)

	assert.Same(t, event, normalized)
	assertSloopCanReadEvent(t, toJson(t, normalized))
}

func Test_normalizeEvent_SeriesAndEventTime(t *testing.T) {
	event := toUnstructured(t, `{
  "apiVersion": "v1",
  "kind": "Event",
  "metadata": {"name": "my-app.1", "namespace": "default"},
  "regarding": {"kind": "Pod", "namespace": "default", "name": "my-app"},
  "reason": "BackOff",
  "eventTime": "2026-10-09T20:14:58.123456Z",
  "series": {"count": 7, "lastObservedTime": "2026-10-09T20:20:00.000000Z"}
}`)
	eventInfo, err := kubeextractor.ExtractEventInfo(toJson(t, normalizeEvent(event)))
	assert.NoError(t, err)
	assert.Equal(t, 7, eventInfo.Count)
	assert.Equal(t, time.Date(2026, 10, 9, 20, 14, 58, 123456000, time.UTC), eventInfo.FirstTimestamp)
	assert.Equal(t, time.Date(2026, 10, 9, 20, 20, 0, 0, time.UTC), eventInfo.LastTimestamp)
}

func Test_normalizeEvent_SingleEventTime(t *testing.T) {
	event := toUnstructured(t, `{
  "apiVersion": "v1",
  "kind": "Event",
  "metadata": {"name": "my-app.1", "namespace": "default"},
  "regarding": {"kind": "Pod", "namespace": "default", "name": "my-app"},
  "reason": "Scheduled",
  "eventTime": "2026-10-09T20:14:58.000000Z"
}`)
	eventInfo, err := kubeextractor.ExtractEventInfo(toJson(t, normalizeEvent(event)))
	assert.NoError(t, err)
	assert.Equal(t, 1, eventInfo.Count)
	assert.Equal(t, eventInfo.FirstTimestamp, eventInfo.LastTimestamp)
	assert.False(t, eventInfo.LastTimestamp.IsZero())
}

func Test_processUpdate_EventsV1Event(t *testing.T) {
	outChan := make(chan typed.KubeWatchResult, 5)
	kw := &kubeWatcherImpl{protection: &sync.Mutex{}, outchan: outChan}

	kw.processUpdate("Event", toUnstructured(t, eventsV1Payload), &typed.KubeWatchResult{Kind: "Event"}, true)
	result := <-outChan
	assert.Equal(t, "Event", result.Kind)
	assertSloopCanReadEvent(t, result.Payload)

	verifyChannelEmpty(t, outChan)
}

// Excluding an unstructured object used to panic while logging its name
func Test_processUpdate_ExcludedUnstructured(t *testing.T) {
	outChan := make(chan typed.KubeWatchResult, 5)
	exclusionRules := map[string][]any{"Event": {map[string]any{"==": []any{map[string]any{"var": "reason"}, "Pulling"}}}}
	kw := &kubeWatcherImpl{protection: &sync.Mutex{}, outchan: outChan, exclusionRules: exclusionRules}

	kw.processUpdate("Event", toUnstructured(t, eventsV1Payload), &typed.KubeWatchResult{Kind: "Event"}, true)
	verifyChannelEmpty(t, outChan)
}

// End to end: an events.k8s.io/v1 shaped Event from the API server reaches the out channel in core/v1 shape
func Test_bigPictureEventsV1Event(t *testing.T) {
	dynamicClient := newTestDynamicClient()
	outChan := make(chan typed.KubeWatchResult, 5)
	kw, err := NewKubeWatcherSource(kubernetesFake.NewSimpleClientset(), dynamicClient, outChan, 30*time.Minute, false, 10*time.Second, "url", "", true, map[string][]any{})
	assert.NoError(t, err)

	_, err = dynamicClient.Resource(eventsGvr).Namespace("default").Create(context.TODO(), toUnstructured(t, eventsV1Payload), metav1.CreateOptions{})
	assert.NoError(t, err)

	select {
	case result := <-outChan:
		assert.Equal(t, "Event", result.Kind)
		assertSloopCanReadEvent(t, result.Payload)
	case <-time.After(10 * time.Second):
		assert.Fail(t, "timed out waiting for the Event")
	}

	kw.Stop()
}
