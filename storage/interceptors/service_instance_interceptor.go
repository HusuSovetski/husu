/*
 * Copyright 2018 The Service Manager Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package interceptors

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Peripli/service-manager/operations"

	"github.com/Peripli/service-manager/pkg/util"

	"github.com/Peripli/service-manager/pkg/log"

	"github.com/Peripli/service-manager/pkg/query"

	"github.com/Peripli/service-manager/pkg/types"
	osbc "github.com/kubernetes-sigs/go-open-service-broker-client/v2"

	"github.com/Peripli/service-manager/storage"
)

//async provisioning
// when broker responds sync
// and succeeds
// and fails
// with non-orphan-mitigatable error
// with orphan--mitigatable error
// when orphan mitigation suceeds
// when orphan mitigation fails

// when broker responds async (atleast 3 polls required)
// and succeeds on 3rd poll
// and fails on 3rd poll
// with non-orphan-mitigatable error
// with orphan--mitigatable error
// when orphan mitigation suceeds on 3rd poll
// when orphan mitigation fails on 3rd poll

// any create/update will return async op in progress error if the op in SMDB is still in progress
// and will internally try to schedule continuation job

// schedulers do reporting by updating updated_at of op/resource
// job that does polling timeouts due to jobtimeout(1d) or due to sm restart (every 10min) -> reporting stops
// maintainer either via pgnotify or by polling every reporting internal*2 mins finds some operations for which report is missing

// maintainer triggers update on the resource, each resource will have logic in it
// about what to do when its last operation is in progress
// update instance will have to

// move op create and op inprogress lock to base_controller
// queue instance create
// interceptor tries to create
// if error queue delete with orphan mitigation true and asyncopinprogress false
// if mitigation is async queue delete with orphan mitigation true and asyncopinprogress true
// if has to continue polling, requeue same as above (such polling requeues need pickup delay)
// if async queue create with asyncopinprogress true
// if has to continue polling queue delayed create with asyncinprogress true
// scheduler report back
//if sm restarts
//(regularly) maintainer reads all in progress operations older than job timeout and updates(optimistically locks) them
//for the once that he managed to lock, schedule jobs with double the timeout in the previous attempt
// (regularly) clean up operations older than a week as well as any resources related to them (if its  create or delete or unusable +updater)
// alternatively, maintainer can use db row locking or advisory locking and does rescheduling of jobs
// reschedule na job ? maintainer ?
// delete

const ServiceInstanceCreateInterceptorProviderName = "ServiceInstanceCreateInterceptorProvider"

// ServiceInstanceCreateInterceptorProvider provides an interceptor that notifies the actual broker about instance creation
type ServiceInstanceCreateInterceptorProvider struct {
	OSBClientCreateFunc  osbc.CreateFunc
	Repository           storage.TransactionalRepository
	TenantKey            string
	PollingInterval      time.Duration
	MaxParallelDeletions int
}

func (p *ServiceInstanceCreateInterceptorProvider) Provide() storage.CreateAroundTxInterceptor {
	return &ServiceInstanceInterceptor{
		osbClientCreateFunc:  p.OSBClientCreateFunc,
		repository:           p.Repository,
		tenantKey:            p.TenantKey,
		pollingInterval:      p.PollingInterval,
		maxParallelDeletions: p.MaxParallelDeletions,
	}
}

func (c *ServiceInstanceCreateInterceptorProvider) Name() string {
	return ServiceInstanceCreateInterceptorProviderName
}

const ServiceInstanceUpdateInterceptorProviderName = "ServiceInstanceUpdateInterceptorProvider"

// ServiceInstanceUpdateInterceptorProvider provides an interceptor that notifies the actual broker about instance updates
type ServiceInstanceUpdateInterceptorProvider struct {
	OSBClientCreateFunc  osbc.CreateFunc
	Repository           storage.TransactionalRepository
	TenantKey            string
	PollingInterval      time.Duration
	MaxParallelDeletions int
}

func (p *ServiceInstanceUpdateInterceptorProvider) Provide() storage.UpdateAroundTxInterceptor {
	return &ServiceInstanceInterceptor{
		osbClientCreateFunc:  p.OSBClientCreateFunc,
		repository:           p.Repository,
		tenantKey:            p.TenantKey,
		pollingInterval:      p.PollingInterval,
		maxParallelDeletions: p.MaxParallelDeletions,
	}
}

func (c *ServiceInstanceUpdateInterceptorProvider) Name() string {
	return ServiceInstanceUpdateInterceptorProviderName
}

const ServiceInstanceDeleteInterceptorProviderName = "ServiceInstanceDeleteInterceptorProvider"

// ServiceInstanceDeleteInterceptorProvider provides an interceptor that notifies the actual broker about instance deletion
type ServiceInstanceDeleteInterceptorProvider struct {
	OSBClientCreateFunc  osbc.CreateFunc
	Repository           storage.TransactionalRepository
	TenantKey            string
	PollingInterval      time.Duration
	MaxParallelDeletions int
}

func (p *ServiceInstanceDeleteInterceptorProvider) Provide() storage.DeleteAroundTxInterceptor {
	return &ServiceInstanceInterceptor{
		osbClientCreateFunc:  p.OSBClientCreateFunc,
		repository:           p.Repository,
		tenantKey:            p.TenantKey,
		pollingInterval:      p.PollingInterval,
		maxParallelDeletions: p.MaxParallelDeletions,
	}
}

func (c *ServiceInstanceDeleteInterceptorProvider) Name() string {
	return ServiceInstanceDeleteInterceptorProviderName
}

type ServiceInstanceInterceptor struct {
	osbClientCreateFunc  osbc.CreateFunc
	repository           storage.TransactionalRepository
	tenantKey            string
	pollingInterval      time.Duration
	maxParallelDeletions int
}

func (i *ServiceInstanceInterceptor) AroundTxCreate(f storage.InterceptCreateAroundTxFunc) storage.InterceptCreateAroundTxFunc {
	return func(ctx context.Context, obj types.Object) (types.Object, error) {
		instance := obj.(*types.ServiceInstance)
		if instance.PlatformID != types.SMPlatform {
			return f(ctx, obj)
		}

		operation, found := operations.GetFromContext(ctx)
		if !found {
			return nil, fmt.Errorf("operation missing from context")
		}

		osbClient, broker, service, plan, err := i.prepare(ctx, instance)
		if err != nil {
			return nil, err
		}

		var provisionResponse *osbc.ProvisionResponse
		if !operation.Reschedule {
			provisionRequest := &osbc.ProvisionRequest{
				InstanceID:        instance.GetID(),
				AcceptsIncomplete: true,
				ServiceID:         service.CatalogID,
				PlanID:            plan.CatalogID,
				OrganizationGUID:  "-",
				SpaceGUID:         "-",
				Parameters:        instance.Parameters,
				Context: map[string]interface{}{
					"platform": "service-manager",
				},
				//TODO no OI for SM platform yet
				OriginatingIdentity: nil,
			}
			if len(i.tenantKey) != 0 {
				if tenantValue, ok := instance.GetLabels()[i.tenantKey]; ok {
					provisionRequest.Context[i.tenantKey] = tenantValue
				}
			}

			log.C(ctx).Infof("Sending provision request %+v to broker with name %s", provisionRequest)
			provisionResponse, err = osbClient.ProvisionInstance(provisionRequest)
			if err != nil {
				brokerError := &util.HTTPError{
					ErrorType:   "BrokerError",
					Description: fmt.Sprintf("Failed provisioning request %+v: %s", provisionRequest, err),
				}
				if i.shouldStartOrphanMitigation(err) {
					operation.DeletionScheduled = time.Now()
					operation.Reschedule = false
					if _, err := i.repository.Update(ctx, operation, query.LabelChanges{}); err != nil {
						return nil, fmt.Errorf("failed to update operation with id %s to schedule orphan mitigation after broker error %s: %s", operation.ID, brokerError, err)
					}
				}
				return nil, brokerError
			}

			if provisionResponse.DashboardURL != nil {
				dashboardURL := *provisionResponse.DashboardURL
				instance.DashboardURL = dashboardURL
			}

			if provisionResponse.Async {
				log.C(ctx).Infof("Successful asynchronous provisioning request %+v to broker %s returned response %+v",
					provisionRequest, broker.Name, provisionResponse)
				operation.Reschedule = true
				if _, err := i.repository.Update(ctx, instance, query.LabelChanges{}); err != nil {
					return nil, fmt.Errorf("failed to update operation with id %s to mark that next execution should be a reschedule", instance.ID)
				}
			} else {
				log.C(ctx).Infof("Successful synchronous provisioning %+v to broker %s returned response %+v",
					provisionRequest, broker.Name, provisionResponse)

			}
		}

		object, err := f(ctx, obj)
		if err != nil {
			return nil, err
		}
		instance = object.(*types.ServiceInstance)

		if operation.Reschedule {
			if err := i.pollServiceInstance(ctx, osbClient, instance, operation, broker.ID, service.CatalogID, plan.CatalogID, provisionResponse.OperationKey, true); err != nil {
				return nil, err
			}
		}

		return instance, nil
	}
}

// TODO Update of instances in SM is not yet implemented
func (i *ServiceInstanceInterceptor) AroundTxUpdate(h storage.InterceptUpdateAroundTxFunc) storage.InterceptUpdateAroundTxFunc {
	return h
}

func (i *ServiceInstanceInterceptor) AroundTxDelete(f storage.InterceptDeleteAroundTxFunc) storage.InterceptDeleteAroundTxFunc {
	return func(ctx context.Context, deletionCriteria ...query.Criterion) error {
		instances, err := i.repository.List(ctx, types.ServiceInstanceType, deletionCriteria...)
		if err != nil {
			return fmt.Errorf("failed to get instances for deletion: %s", err)
		}

		if instances.Len() > 1 {
			return fmt.Errorf("deletion of multiple instances is not supported")
		}

		if instances.Len() != 0 {
			instance := instances.ItemAt(0).(*types.ServiceInstance)
			if instance.PlatformID != types.SMPlatform {
				return f(ctx, deletionCriteria...)
			}

			operation, found := operations.GetFromContext(ctx)
			if !found {
				return fmt.Errorf("operation missing from context")
			}

			if err := i.deleteSingleInstance(ctx, instance, operation); err != nil {
				return err
			}
		}

		if err := f(ctx, deletionCriteria...); err != nil {
			return err
		}

		return nil
	}
}

func (i *ServiceInstanceInterceptor) deleteSingleInstance(ctx context.Context, instance *types.ServiceInstance, operation *types.Operation) error {
	byServiceInstanceID := query.ByField(query.EqualsOperator, "service_instance_id", instance.ID)
	var bindingsCount int
	var err error
	if bindingsCount, err = i.repository.Count(ctx, types.ServiceBindingType, byServiceInstanceID); err != nil {
		return fmt.Errorf("could not fetch bindings for instance with id %s", instance.ID)
	}
	if bindingsCount > 0 {
		return &util.HTTPError{
			ErrorType:   "BadRequest",
			Description: fmt.Sprintf("could not delete instance due to %d existing bindings", bindingsCount),
		}
	}

	osbClient, broker, service, plan, err := i.prepare(ctx, instance)
	if err != nil {
		return err
	}

	var deprovisionResponse *osbc.DeprovisionResponse
	if !operation.Reschedule {
		deprovisionRequest := &osbc.DeprovisionRequest{
			InstanceID:        instance.GetID(),
			AcceptsIncomplete: true,
			ServiceID:         service.CatalogID,
			PlanID:            plan.CatalogID,
			//TODO no OI for SM platform yet
			OriginatingIdentity: nil,
		}

		log.C(ctx).Infof("Sending deprovision request %+v to broker with name %s", deprovisionRequest, broker.Name)
		deprovisionResponse, err = osbClient.DeprovisionInstance(deprovisionRequest)
		if err != nil {
			if osbc.IsGoneError(err) {
				log.C(ctx).Infof("Synchronous deprovisioning %+v to broker %s returned 410 GONE and is considered success",
					deprovisionRequest, broker.Name)
				return nil
			}
			brokerError := &util.HTTPError{
				ErrorType:   "BrokerError",
				Description: fmt.Sprintf("Failed deprovisioning request %+v: %s", deprovisionRequest, err),
			}
			if i.shouldStartOrphanMitigation(err) {
				operation.DeletionScheduled = time.Now()
				operation.Reschedule = false
				if _, err := i.repository.Update(ctx, operation, query.LabelChanges{}); err != nil {
					return fmt.Errorf("failed to update operation with id %s to schedule orphan mitigation after broker error %s: %s", operation.ID, brokerError, err)
				}
			}
			return brokerError
		}

		if deprovisionResponse.Async {
			log.C(ctx).Infof("Successful asynchronous deprovisioning request %+v to broker %s returned response %+v",
				deprovisionRequest, broker.Name, deprovisionResponse)
			operation.Reschedule = true
			if _, err := i.repository.Update(ctx, instance, query.LabelChanges{}); err != nil {
				return fmt.Errorf("failed to update operation with id %s to mark that rescheduling is possible", operation.ID)
			}
		} else {
			log.C(ctx).Infof("Successful synchronous deprovisioning %+v to broker %s returned response %+v",
				deprovisionRequest, broker.Name, deprovisionResponse)
		}
	}

	if operation.Reschedule {
		if err := i.pollServiceInstance(ctx, osbClient, instance, operation, broker.ID, service.CatalogID, plan.CatalogID, deprovisionResponse.OperationKey, true); err != nil {
			return err
		}
	}

	return nil
}

func (i *ServiceInstanceInterceptor) pollServiceInstance(ctx context.Context, osbClient osbc.Client, instance *types.ServiceInstance, operation *types.Operation, brokerID, serviceCatalogID, planCatalogID string, operationKey *osbc.OperationKey, enableOrphanMitigation bool) error {
	pollingRequest := &osbc.LastOperationRequest{
		InstanceID:   instance.ID,
		ServiceID:    &serviceCatalogID,
		PlanID:       &planCatalogID,
		OperationKey: operationKey,
		//TODO no OI for SM platform yet
		OriginatingIdentity: nil,
	}

	ticker := time.NewTicker(i.pollingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.C(ctx).Errorf("Terminating poll last operation for instance with id %s and name %s due to context done event", instance.ID, instance.Name)
			//operation should be kept in progress in this case
			return nil
		case <-ticker.C:
			log.C(ctx).Infof("Sending poll last operation request %+v for instance with id %s and name %s", pollingRequest, instance.ID, instance.Name)
			pollingResponse, err := osbClient.PollLastOperation(pollingRequest)
			if err != nil {
				return &util.HTTPError{
					ErrorType: "BrokerError",
					Description: fmt.Sprintf("Failed poll last operation request %+v for instance with id %s and name %s: %s",
						pollingRequest, instance.ID, instance.Name, err),
				}
			}
			switch pollingResponse.State {
			case osbc.StateInProgress:
				log.C(ctx).Infof("Polling of instance still in progress. Rescheduling polling last operation request %+v to broker with name %s for provisioning of instance with id %s and name %s...", pollingRequest, instance.ID, instance.Name)

			case osbc.StateSucceeded:
				log.C(ctx).Infof("Successfully finished polling operation for instance with id %s and name %s", instance.ID, instance.Name)

				operation.Reschedule = false
				if _, err := i.repository.Update(ctx, operation, query.LabelChanges{}); err != nil {
					return fmt.Errorf("failed to update operation with id %s to mark that next execution should be a reschedule", operation.ID)
				}

				return nil
			case osbc.StateFailed:
				log.C(ctx).Infof("Failed polling operation for instance with id %s and name %s", instance.ID, instance.Name)
				operation.Reschedule = false
				if enableOrphanMitigation {
					operation.DeletionScheduled = time.Now()
				}
				if _, err := i.repository.Update(ctx, instance, query.LabelChanges{}); err != nil {
					return fmt.Errorf("failed to update operation with id %s after failed of last operation for instance with id %s", operation.ID, instance.ID)
				}

				errDescription := ""
				if pollingResponse.Description != nil {
					errDescription = *pollingResponse.Description
				} else {
					errDescription = "no description provided by broker"
				}
				return &util.HTTPError{
					ErrorType:   "BrokerError",
					Description: fmt.Sprintf("failed polling operation for instance with id %s and name %s due to polling last operation error: %s", instance.ID, instance.Name, errDescription),
				}
			default:
				return fmt.Errorf("invalid state during poll last operation for instance with id %s and name %s: %s", instance.ID, instance.Name, pollingResponse.State)
			}
		}
	}
}

func (i *ServiceInstanceInterceptor) prepare(ctx context.Context, instance *types.ServiceInstance) (osbc.Client, *types.ServiceBroker, *types.ServiceOffering, *types.ServicePlan, error) {
	planObject, err := i.repository.Get(ctx, types.ServicePlanType, query.ByField(query.EqualsOperator, "id", instance.ServicePlanID))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	plan := planObject.(*types.ServicePlan)

	serviceObject, err := i.repository.Get(ctx, types.ServiceOfferingType, query.ByField(query.EqualsOperator, "id", plan.ServiceOfferingID))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	service := serviceObject.(*types.ServiceOffering)

	brokerObject, err := i.repository.Get(ctx, types.ServiceBrokerType, query.ByField(query.EqualsOperator, "id", service.BrokerID))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	broker := brokerObject.(*types.ServiceBroker)
	osbClient, err := i.osbClientCreateFunc(&osbc.ClientConfiguration{
		Name:       broker.Name + " broker client",
		URL:        broker.BrokerURL,
		APIVersion: osbc.LatestAPIVersion(),
		AuthConfig: &osbc.AuthConfig{
			BasicAuthConfig: &osbc.BasicAuthConfig{
				Username: broker.Credentials.Basic.Username,
				Password: broker.Credentials.Basic.Password,
			},
		},
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return osbClient, broker, service, plan, nil
}

func (i *ServiceInstanceInterceptor) shouldStartOrphanMitigation(err error) bool {
	if httpError, ok := osbc.IsHTTPError(err); ok {
		statusCode := httpError.StatusCode
		is2XX := statusCode >= 200 && statusCode < 300
		is5XX := statusCode >= 500 && statusCode < 600
		return (is2XX && statusCode != http.StatusOK) ||
			statusCode == http.StatusRequestTimeout ||
			is5XX
	}

	if urlErr, ok := err.(*url.Error); ok && urlErr.Timeout() {
		return true
	}

	return false
}