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

package info_test

import (
	"net/http"
	"testing"

	"github.com/Peripli/service-manager/api/info"
	"github.com/Peripli/service-manager/test/common"
	. "github.com/onsi/ginkgo"
)

func TestInfo(t *testing.T) {
	RunSpecs(t, "Info Suite")
}

var _ = Describe("Info API", func() {

	var ctx *common.TestContext

	BeforeSuite(func() {
		ctx = common.NewTestContext(nil)
	})

	AfterSuite(func() {
		ctx.Cleanup()
	})

	Describe("Get info handler", func() {
		It("Returns correct response", func() {
			ctx.SM.GET(info.URL).
				Expect().
				Status(http.StatusOK).
				JSON().Object().Keys().Contains("token_issuer_url")
		})
	})
})
