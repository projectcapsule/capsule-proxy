package webserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func handleError(w http.ResponseWriter, err error, message string) {
	message = fmt.Sprintf("%s: %s", message, err.Error())
	w.Header().Set("content-type", "application/json")
	status := &metav1.Status{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Status",
			APIVersion: "v1",
		},
		Message: message,
		Reason:  metav1.StatusReasonInternalError,
	}
	b, _ := json.Marshal(status)
	_, _ = w.Write(b)
	panic(message)
}

func handleNotFound(w http.ResponseWriter, message string, details *metav1.StatusDetails) {
	w.Header().Set("content-type", "application/json")
	status := &metav1.Status{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Status",
			APIVersion: "v1",
		},
		Message: message,
		Reason:  metav1.StatusReasonNotFound,
		Status:  metav1.StatusFailure,
		Details: details,
		Code:    http.StatusNotFound,
	}
	b, _ := json.Marshal(status)
	_, _ = w.Write(b)
	panic(message)
}
