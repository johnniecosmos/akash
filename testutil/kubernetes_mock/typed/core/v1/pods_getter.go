// Code generated by mockery v1.0.0. DO NOT EDIT.

package kubernetes_mocks

import (
	mock "github.com/stretchr/testify/mock"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// PodsGetter is an autogenerated mock type for the PodsGetter type
type PodsGetter struct {
	mock.Mock
}

// Pods provides a mock function with given fields: namespace
func (_m *PodsGetter) Pods(namespace string) v1.PodInterface {
	ret := _m.Called(namespace)

	var r0 v1.PodInterface
	if rf, ok := ret.Get(0).(func(string) v1.PodInterface); ok {
		r0 = rf(namespace)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(v1.PodInterface)
		}
	}

	return r0
}
