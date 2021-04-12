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

package osb_test

import (
	"context"
	"fmt"
	"github.com/gofrs/uuid"
	"net/http"

	"github.com/Peripli/service-manager/pkg/query"
	"github.com/Peripli/service-manager/test"
	"github.com/Peripli/service-manager/test/common"

	"github.com/Peripli/service-manager/pkg/types"
	"github.com/Peripli/service-manager/pkg/web"
	. "github.com/Peripli/service-manager/test/common"
	"github.com/gavv/httpexpect"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Instance Sharing", func() {

	Context("References", func() {
		var platform *types.Platform
		var platformJSON common.Object
		var referenceInstanceID string

		JustBeforeEach(func() {
			brokerServer.ServiceInstanceHandler = parameterizedHandler(http.StatusCreated, `{}`)
			utils.BrokerWithTLS.BrokerServer.ServiceInstanceHandler = parameterizedHandler(http.StatusCreated, `{}`)
			platform = common.RegisterPlatformInSM(platformJSON, ctx.SMWithOAuth, map[string]string{})

			utils.SetAuthContext(ctx.SMWithOAuth).AddPlanVisibilityForPlatform(utils.SelectBroker(&utils.BrokerWithTLS).GetPlanCatalogId(0, 0), platform.ID, organizationGUID)
			utils.SetAuthContext(ctx.SMWithOAuth).AddPlanVisibilityForPlatform(plan1CatalogID, platform.ID, organizationGUID)

			SMWithBasic := &common.SMExpect{Expect: ctx.SM.Builder(func(req *httpexpect.Request) {
				username, password := platform.Credentials.Basic.Username, platform.Credentials.Basic.Password
				req.WithBasicAuth(username, password).WithClient(ctx.HttpClient)
			})}

			username, password := test.RegisterBrokerPlatformCredentials(SMWithBasic, brokerID)
			utils.SetAuthContext(SMWithBasic).RegisterPlatformToBroker(username, password, utils.BrokerWithTLS.ID)
			ctx.SMWithBasic.SetBasicCredentials(ctx, username, password)
		})

		AfterEach(func() {
			err := ctx.SMRepository.Delete(context.TODO(), types.BrokerPlatformCredentialType,
				query.ByField(query.EqualsOperator, "platform_id", platform.ID))
			Expect(err).ToNot(HaveOccurred())

			ctx.SMWithOAuth.DELETE(web.VisibilitiesURL + "?fieldQuery=" + fmt.Sprintf("platform_id eq '%s'", platform.ID))
			ctx.SMWithOAuth.DELETE(web.PlatformsURL + "/" + platform.ID).Expect().Status(http.StatusOK)
		})

		When("Provision", func() {
			Context("in CF platform", func() {
				BeforeEach(func() {
					platformJSON = common.MakePlatform("cf-platform", "cf-platform", "cloudfoundry", "test-platform-cf")
				})
				It("creates reference instance successfully", func() {
					_, referenceInstanceID = executeProvisionTest(platform)
				})

			})

			Context("in K8S platform", func() {
				BeforeEach(func() {
					platformJSON = common.MakePlatform("k8s-platform", "k8s-platform", "kubernetes", "test-platform-k8s")
				})
				It("creates reference instance successfully", func() {
					_, referenceInstanceID = executeProvisionTest(platform)
				})
			})
		})

		When("Deprovision", func() {

			Context("in CF platform", func() {
				BeforeEach(func() {
					platformJSON = common.MakePlatform("cf-platform", "cf-platform", "cloudfoundry", "test-platform-cf")
				})

				AfterEach(func() {
					VerifyResourceDoesNotExist(ctx.SMWithOAuthForTenant, ResourceExpectations{
						ID:   referenceInstanceID,
						Type: types.ServiceInstanceType,
					})

				})

				It("deletes reference instance successfully", func() {
					_, referenceInstanceID = executeProvisionTest(platform)
					deleteInstance(referenceInstanceID)
				})
			})

			Context("in K8S platform", func() {
				BeforeEach(func() {
					platformJSON = common.MakePlatform("k8s-platform", "k8s-platform", "kubernetes", "test-platform-k8s")
				})

				It("deletes reference instance successfully", func() {
					_, referenceInstanceID = executeProvisionTest(platform)
					resp := ctx.SMWithBasic.DELETE(smBrokerURL+"/v2/service_instances/"+referenceInstanceID).
						WithHeader(brokerAPIVersionHeaderKey, brokerAPIVersionHeaderValue).
						WithQuery("async", "false").
						Expect().Status(http.StatusOK)
					fmt.Print(resp)
					//deleteInstance(referenceInstanceID)
				})

			})
		})

		When("Bind", func() {

			AfterEach(func() {
			})
			Context("in CF platform", func() {
				var cfSharedInstanceID string
				var k8sReferenceInstanceID string
				BeforeEach(func() {
					platformJSON = common.MakePlatform("cf-platform", "cf-platform", "cloudfoundry", "test-platform-cf")
				})
				It("binds reference instance successfully", func() {
					_, referenceInstanceID = executeProvisionTest(platform)
					bindingID := createBinding(referenceInstanceID)

					ctx.SMWithOAuth.GET(web.ServiceBindingsURL+"/"+bindingID).
						Expect().
						Status(http.StatusOK).
						JSON().
						Object().ContainsKey("service_instance_id").
						ValueEqual("service_instance_id", referenceInstanceID)
				})
				It("binds reference instance successfully from different platform", func() {
					cfSharedInstanceID = createAndShareInstance()

					k8SPlatformJSON := common.MakePlatform("k8s-platform", "k8s-platform", "kubernetes", "test-platform-k8s")
					k8sPlatform := common.RegisterPlatformInSM(k8SPlatformJSON, ctx.SMWithOAuth, map[string]string{})

					k8sReferenceInstanceID = createReferenceInstance(k8sPlatform.ID, cfSharedInstanceID)

					bindingID := createBinding(k8sReferenceInstanceID)
					ctx.SMWithOAuth.GET(web.ServiceBindingsURL+"/"+bindingID).
						Expect().
						Status(http.StatusOK).
						JSON().
						Object().ContainsKey("service_instance_id").
						ValueEqual("service_instance_id", k8sReferenceInstanceID)

					verifyOperationExists(operationExpectations{
						Type:         types.CREATE,
						State:        types.SUCCEEDED,
						ResourceID:   bindingID,
						ResourceType: "/v1/service_bindings",
						ExternalID:   "",
					})

					VerifyResourceExists(ctx.SMWithOAuthForTenant, ResourceExpectations{
						ID:    bindingID,
						Type:  types.ServiceBindingType,
						Ready: true,
					})

				})
			})
			Context("in K8S platform", func() {
				BeforeEach(func() {
					platformJSON = common.MakePlatform("k8s-platform", "k8s-platform", "kubernetes", "test-platform-k8s")
				})
				It("binds reference instance successfully", func() {
					_, referenceInstanceID = executeProvisionTest(platform)
					bindingID := createBinding(referenceInstanceID)

					ctx.SMWithOAuth.GET(web.ServiceBindingsURL+"/"+bindingID).
						Expect().
						Status(http.StatusOK).
						JSON().
						Object().ContainsKey("service_instance_id").
						ValueEqual("service_instance_id", referenceInstanceID)
				})
			})
		})

		When("Unbind", func() {

			AfterEach(func() {
			})
			Context("in CF platform", func() {
				var bindingID string
				BeforeEach(func() {
					platformJSON = common.MakePlatform("cf-platform", "cf-platform", "cloudfoundry", "test-platform-cf")
				})
				JustBeforeEach(func() {
					_, referenceInstanceID = executeProvisionTest(platform)
					bindingID = createBinding(referenceInstanceID)
					ctx.SMWithOAuth.GET(web.ServiceBindingsURL+"/"+bindingID).
						Expect().
						Status(http.StatusOK).
						JSON().
						Object().ContainsKey("service_instance_id").
						ValueEqual("service_instance_id", referenceInstanceID)
				})
				It("unbinds reference instance successfully", func() {
					ctx.SMWithBasic.DELETE(smBrokerURL+"/v2/service_instances/"+referenceInstanceID+"/service_bindings/"+bindingID).
						WithHeader(brokerAPIVersionHeaderKey, brokerAPIVersionHeaderValue).
						Expect().
						Status(http.StatusOK).
						JSON().
						Object()

					ctx.SMWithOAuth.GET(web.ServiceBindingsURL + "/" + bindingID).
						Expect().Status(http.StatusNotFound)

					verifyOperationExists(operationExpectations{
						Type:         types.DELETE,
						State:        types.SUCCEEDED,
						ResourceID:   bindingID,
						ResourceType: "/v1/service_bindings",
						ExternalID:   "",
					})
				})
			})
		})
	})
})

func createBinding(instanceID string) string {
	UUID, err := uuid.NewV4()
	if err != nil {
		panic(err)
	}
	bindingID := UUID.String()

	body := provisionRequestBodyMap()()
	ctx.SMWithBasic.PUT(smBrokerURL+"/v2/service_instances/"+instanceID+"/service_bindings/"+bindingID).
		WithHeader(brokerAPIVersionHeaderKey, brokerAPIVersionHeaderValue).
		WithJSON(body).
		Expect().
		Status(http.StatusCreated)

	return bindingID
}

func deleteInstance(instanceID string) *httpexpect.Response {
	resp := ctx.SMWithBasic.DELETE(smBrokerURL+"/v2/service_instances/"+instanceID).
		WithHeader(brokerAPIVersionHeaderKey, brokerAPIVersionHeaderValue).
		WithQuery("async", "false")
	return resp.
		Expect().Status(http.StatusOK)
}

func executeProvisionTest(platform *types.Platform) (string, string) {
	sharedInstanceID := createAndShareInstance()
	VerifyResourceExists(ctx.SMWithOAuthForTenant, ResourceExpectations{
		ID:    sharedInstanceID,
		Type:  types.ServiceInstanceType,
		Ready: true,
	})
	referenceInstanceID := createReferenceInstance(platform.ID, sharedInstanceID)
	obj := VerifyResourceExists(ctx.SMWithOAuthForTenant, ResourceExpectations{
		ID:    referenceInstanceID,
		Type:  types.ServiceInstanceType,
		Ready: true,
	})
	obj.
		ContainsKey("platform_id").
		ValueEqual("platform_id", platform.ID)
	return sharedInstanceID, referenceInstanceID
}

func createReferenceInstance(platformID string, sharedInstanceID string) string {
	UUID, err := uuid.NewV4()
	if err != nil {
		panic(err)
	}
	instanceID := UUID.String()

	referencePlan := GetReferencePlanOfExistingPlan(ctx, "catalog_id", plan1CatalogID)
	referenceProvisionBody := buildReferenceProvisionBody(referencePlan.CatalogID, sharedInstanceID)
	utils.SetAuthContext(ctx.SMWithOAuth).AddPlanVisibilityForPlatform(referencePlan.CatalogID, platformID, organizationGUID)
	ctx.SMWithBasic.PUT(smBrokerURL+"/v2/service_instances/"+instanceID).
		WithQuery("async", "false").
		WithHeader(brokerAPIVersionHeaderKey, brokerAPIVersionHeaderValue).
		WithJSON(referenceProvisionBody).
		Expect().Status(http.StatusCreated)
	return instanceID
}

func createAndShareInstance() string {
	UUID, err := uuid.NewV4()
	if err != nil {
		panic(err)
	}
	sharedInstanceID := UUID.String()

	resp := ctx.SMWithBasic.PUT(smBrokerURL+"/v2/service_instances/"+sharedInstanceID).
		WithHeader(brokerAPIVersionHeaderKey, brokerAPIVersionHeaderValue).
		WithJSON(provisionRequestBodyMapWith("plan_id", plan1CatalogID)()).
		Expect().Status(http.StatusCreated)
	fmt.Print(resp)
	ShareInstanceOnDB(ctx.SMRepository, context.TODO(), sharedInstanceID)

	return sharedInstanceID
}

func buildReferenceProvisionBody(planID, sharedInstanceID string) Object {
	return Object{
		"service_id":        service1CatalogID,
		"plan_id":           planID,
		"organization_guid": organizationGUID,
		"space_guid":        "aaaa1234-da91-4f12-8ffa-b51d0336aaaa",
		"parameters": Object{
			"referenced_instance_id": sharedInstanceID,
		},
		"context": Object{
			"platform":          "cloudfoundry",
			"organization_guid": organizationGUID,
			"organization_name": "system",
			"space_guid":        "aaaa1234-da91-4f12-8ffa-b51d0336aaaa",
			"space_name":        "development",
			"instance_name":     "reference-instance",
			TenantIdentifier:    TenantValue,
		},
		"maintenance_info": Object{
			"version": "old",
		},
	}
}
