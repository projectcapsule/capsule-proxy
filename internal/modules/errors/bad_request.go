package errors

import (
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type badRequest struct {
	message string
	details *metav1.StatusDetails
}

func NewBadRequest(message error, details *metav1.StatusDetails) error {
	return &badRequest{message: message.Error(), details: details}
}

func (b badRequest) Error() string {
	return b.message
}

func (b badRequest) Status() *metav1.Status {
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Status",
			APIVersion: "v1",
		},
		Reason:  metav1.StatusReasonBadRequest,
		Message: b.message,
		Status:  metav1.StatusFailure,
		Code:    http.StatusBadRequest,
		Details: b.details,
	}
}
