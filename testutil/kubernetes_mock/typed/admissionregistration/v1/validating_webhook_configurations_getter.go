// Code generated by mockery v1.0.0. DO NOT EDIT.

package kubernetes_mocks

import (
	mock "github.com/stretchr/testify/mock"
	v1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
)

// ValidatingWebhookConfigurationsGetter is an autogenerated mock type for the ValidatingWebhookConfigurationsGetter type
type ValidatingWebhookConfigurationsGetter struct {
	mock.Mock
}

// ValidatingWebhookConfigurations provides a mock function with given fields:
func (_m *ValidatingWebhookConfigurationsGetter) ValidatingWebhookConfigurations() v1.ValidatingWebhookConfigurationInterface {
	ret := _m.Called()

	var r0 v1.ValidatingWebhookConfigurationInterface
	if rf, ok := ret.Get(0).(func() v1.ValidatingWebhookConfigurationInterface); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(v1.ValidatingWebhookConfigurationInterface)
		}
	}

	return r0
}