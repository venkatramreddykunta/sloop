/*
 * Copyright (c) 2026, salesforce.com, inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see LICENSE.txt file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package ingress

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Sloop reads Events with the core/v1 field names (involvedObject, message, count, firstTimestamp,
// lastTimestamp, source). Some API servers (for example a proxy that stores events.k8s.io/v1 Events)
// return Events on /api/v1 with only the events.k8s.io/v1 field names:
//
//	{"regarding":{"kind":"Pod",...},"note":"Pulling image","deprecatedCount":3,
//	 "deprecatedFirstTimestamp":"...","deprecatedLastTimestamp":"...","deprecatedSource":{...}}
//
// Decoded as corev1.Event, all of these fields are silently dropped, so the event is stored with an empty
// involvedObject and a count of 0. normalizeEvent copies the events.k8s.io/v1 values into the missing
// core/v1 fields. Events that already have an involvedObject are returned unchanged.
//
// The input comes from the informer cache and must not be modified, so a copy is returned.
func normalizeEvent(event *unstructured.Unstructured) *unstructured.Unstructured {
	if isEmptyField(event.Object, "regarding") || !isEmptyField(event.Object, "involvedObject") {
		return event
	}

	out := event.DeepCopy()
	copyIfMissing(out.Object, "involvedObject", "regarding")
	copyIfMissing(out.Object, "message", "note")
	copyIfMissing(out.Object, "source", "deprecatedSource")
	copyIfMissing(out.Object, "reportingComponent", "reportingController")
	copyIfMissing(out.Object, "count", "deprecatedCount")
	copyIfMissing(out.Object, "firstTimestamp", "deprecatedFirstTimestamp")
	copyIfMissing(out.Object, "lastTimestamp", "deprecatedLastTimestamp")

	// Events created through events.k8s.io/v1 may only have eventTime and series
	copyIfMissing(out.Object, "count", "series", "count")
	copyIfMissing(out.Object, "lastTimestamp", "series", "lastObservedTime")
	copyIfMissing(out.Object, "firstTimestamp", "eventTime")
	copyIfMissing(out.Object, "lastTimestamp", "eventTime")
	if isEmptyField(out.Object, "count") {
		out.Object["count"] = int64(1)
	}
	return out
}

// copyIfMissing sets obj[dst] to the value at the path src when obj[dst] is empty and the src value is not
func copyIfMissing(obj map[string]interface{}, dst string, src ...string) {
	if !isEmptyField(obj, dst) {
		return
	}
	val, found, err := unstructured.NestedFieldNoCopy(obj, src...)
	if err != nil || !found || isEmpty(val) {
		return
	}
	obj[dst] = val
}

func isEmptyField(obj map[string]interface{}, field string) bool {
	return isEmpty(obj[field])
}

func isEmpty(val interface{}) bool {
	switch v := val.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case map[string]interface{}:
		return len(v) == 0
	}
	return false
}
