package utils

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

func JsonEncode(obj runtime.Object, scheme *runtime.Scheme) ([]byte, error) {
	s := json.NewSerializerWithOptions(
		json.DefaultMetaFactory,
		scheme, scheme,
		json.SerializerOptions{
			Yaml:   false,
			Pretty: true,
			Strict: false,
		},
	)

	return runtime.Encode(s, obj)
}
