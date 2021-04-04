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
		Reason:  "Failure",
	}
	b, _ := json.Marshal(status)
	_, _ = w.Write(b)
	panic(message)
}
