/* SPDX-License-Identifier: Apache-2.0 */
/* Copyright(c) 2019 Wind River Systems, Inc. */

package validating

import (
	"context"
	"fmt"
	"github.com/gophercloud/gophercloud/starlingx/inventory/v1/memory"
	"github.com/gophercloud/gophercloud/starlingx/inventory/v1/physicalvolumes"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"

	starlingxv1 "github.com/wind-river/cloud-platform-deployment-manager/pkg/apis/starlingx/v1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

// Webhook response reasons
const AllowedReason string = "allowed to be admitted"

func init() {
	webhookName := "validating-create-update-hostprofile"
	if HandlerMap[webhookName] == nil {
		HandlerMap[webhookName] = []admission.Handler{}
	}
	HandlerMap[webhookName] = append(HandlerMap[webhookName], &HostProfileCreateUpdateHandler{})
}

// HostProfileCreateUpdateHandler handles HostProfile
type HostProfileCreateUpdateHandler struct {
	// API client
	Client client.Client

	// Decoder decodes objects
	Decoder types.Decoder
}

func (h *HostProfileCreateUpdateHandler) validateMemoryFunction(ctx context.Context, node starlingxv1.MemoryNodeInfo, function starlingxv1.MemoryFunctionInfo) (bool, string, error) {
	if function.Function == memory.MemoryFunctionPlatform {
		if starlingxv1.PageSize(function.PageSize) != starlingxv1.PageSize4K {
			return false, "platform memory must be allocated from 4K pages.", nil
		}
	}

	if starlingxv1.PageSize(function.PageSize) == starlingxv1.PageSize4K {
		if function.Function != memory.MemoryFunctionPlatform {
			return false, "4K pages can only be reserved for platform memory.", nil
		}
	}

	return true, AllowedReason, nil
}

func (h *HostProfileCreateUpdateHandler) validateMemoryInfo(ctx context.Context, obj *starlingxv1.HostProfile) (bool, string, error) {
	var allowed = true
	var reason = AllowedReason
	var err error

	for _, n := range obj.Spec.Memory {
		present := make(map[string]bool)
		for _, f := range n.Functions {
			key := fmt.Sprintf("%s-%s", f.Function, f.PageSize)
			if _, ok := present[key]; ok {
				msg := fmt.Sprintf("duplicate memory entries are not allowed for node %d function %s pagesize %s.",
					n.Node, f.Function, f.PageSize)
				return false, msg, nil
			}
			present[key] = true

			allowed, reason, err = h.validateMemoryFunction(ctx, n, f)
			if !allowed || err != nil {
				return allowed, reason, err
			}
		}
	}

	return allowed, reason, err
}

func (h *HostProfileCreateUpdateHandler) validateProcessorInfo(ctx context.Context, obj *starlingxv1.HostProfile) (bool, string, error) {
	for _, n := range obj.Spec.Processors {
		present := make(map[string]bool)
		for _, f := range n.Functions {
			key := f.Function
			if _, ok := present[key]; ok {
				msg := fmt.Sprintf("duplicate processor entries are not allowed for node %d function %s.",
					n.Node, f.Function)
				return false, msg, nil
			}
			present[key] = true
		}
	}

	return true, AllowedReason, nil
}

func (h *HostProfileCreateUpdateHandler) validatePhysicalVolumeInfo(ctx context.Context, obj *starlingxv1.PhysicalVolumeInfo) (bool, string, error) {
	if obj.Type == physicalvolumes.PVTypePartition {
		if obj.Size == nil {
			msg := fmt.Sprintf("partition specifications must include a 'size' attribute")
			return false, msg, nil
		}
	}

	return true, AllowedReason, nil
}

func (h *HostProfileCreateUpdateHandler) validateVolumeGroupInfo(ctx context.Context, obj *starlingxv1.VolumeGroupInfo) (bool, string, error) {
	for _, pv := range obj.PhysicalVolumes {
		allowed, reason, err := h.validatePhysicalVolumeInfo(ctx, &pv)
		if !allowed || err != nil {
			return allowed, reason, err
		}
	}

	return true, AllowedReason, nil
}

func (h *HostProfileCreateUpdateHandler) validateStorageInfo(ctx context.Context, obj *starlingxv1.HostProfile) (bool, string, error) {
	if obj.Spec.Storage.VolumeGroups != nil {
		for _, vg := range *obj.Spec.Storage.VolumeGroups {
			allowed, reason, err := h.validateVolumeGroupInfo(ctx, &vg)
			if !allowed || err != nil {
				return allowed, reason, err
			}
		}
	}

	return true, AllowedReason, nil
}

func (h *HostProfileCreateUpdateHandler) validatingHostProfileFn(ctx context.Context, obj *starlingxv1.HostProfile) (bool, string, error) {
	var allowed = true
	var reason = AllowedReason
	var err error

	if obj.Spec.Base != nil && *obj.Spec.Base == "" {
		return false, "profile base name must not be empty", nil
	}

	if obj.Spec.Memory != nil {
		allowed, reason, err = h.validateMemoryInfo(ctx, obj)
		if !allowed || err != nil {
			return allowed, reason, err
		}
	}

	if obj.Spec.Processors != nil {
		allowed, reason, err = h.validateProcessorInfo(ctx, obj)
		if !allowed || err != nil {
			return allowed, reason, err
		}
	}

	if obj.Spec.Storage != nil {
		allowed, reason, err = h.validateStorageInfo(ctx, obj)
		if !allowed || err != nil {
			return allowed, reason, err
		}
	}

	return allowed, reason, err
}

var _ admission.Handler = &HostProfileCreateUpdateHandler{}

// Handle handles admission requests.
func (h *HostProfileCreateUpdateHandler) Handle(ctx context.Context, req types.Request) types.Response {
	obj := &starlingxv1.HostProfile{}

	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.ErrorResponse(http.StatusBadRequest, err)
	}

	allowed, reason, err := h.validatingHostProfileFn(ctx, obj)
	if err != nil {
		return admission.ErrorResponse(http.StatusInternalServerError, err)
	}
	return admission.ValidationResponse(allowed, reason)
}

var _ inject.Client = &HostProfileCreateUpdateHandler{}

// InjectClient injects the client into the HostProfileCreateUpdateHandler
func (h *HostProfileCreateUpdateHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}

var _ inject.Decoder = &HostProfileCreateUpdateHandler{}

// InjectDecoder injects the decoder into the HostProfileCreateUpdateHandler
func (h *HostProfileCreateUpdateHandler) InjectDecoder(d types.Decoder) error {
	h.Decoder = d
	return nil
}
