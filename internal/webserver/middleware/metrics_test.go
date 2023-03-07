// Copyright 2022 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

//nolint:testpackage
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	model "github.com/prometheus/client_model/go"
)

func dummyHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("hello"))
	w.WriteHeader(http.StatusOK)
}

func newRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil) //nolint:noctx

	return req, err
}

func Test_MetricsMiddleware_RequestCount(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		requestCount int
		path         string
		output       float64
	}{
		{
			name:         "single request count",
			requestCount: 1,
			path:         "/test",
			output:       1,
		},
	}

	//nolint:paralleltest
	for _, test := range testCases {
		router := mux.NewRouter()
		router.HandleFunc(test.path, dummyHandler).Methods("GET")
		router.Use(MetricsMiddleware)

		rw := httptest.NewRecorder()

		for i := 0; i < test.requestCount; i++ {
			req, err := newRequest("GET", test.path)
			if err != nil {
				t.Errorf("failed to create HTTP request object")
			}

			router.ServeHTTP(rw, req)
		}

		t.Run("regular middleware call", func(t *testing.T) {
			ch := make(chan prometheus.Metric)
			go totalRequests.Collect(ch)
			g := (<-ch).(prometheus.Counter)
			result := readVector(g)
			if test.output != result.value {
				t.Errorf("testcase %s failed. expected: %f, got: %f", test.name, test.output, result.value)
			}
		})
	}
}

type metricResult struct {
	value  float64
	labels map[string]string
}

func labels2Map(labels []*model.LabelPair) map[string]string {
	res := map[string]string{}
	for _, l := range labels {
		res[l.GetName()] = l.GetValue()
	}

	return res
}

func readVector(g prometheus.Metric) metricResult {
	m := &model.Metric{}
	_ = g.Write(m)

	return metricResult{
		value:  m.GetCounter().GetValue(),
		labels: labels2Map(m.GetLabel()),
	}
}
