package disabled_query_params_test

import (
	"github.com/Peripli/service-manager/pkg/web"
	"github.com/Peripli/service-manager/test/common"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
	"net/http"
	"testing"
)

func TestDisabledQuery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Disable Query Parameters Tests Suite")
}

var _ = Describe("disable query parameter", func() {

	var (
		ctxBuilder *common.TestContextBuilder
		ctx        *common.TestContext
	)

	Describe("query param is extended", func() {
		AfterEach(func() {
			ctx.Cleanup()
		})

		BeforeEach(func() {
			ctxBuilder = common.NewTestContextBuilderWithSecurity().WithEnvPreExtensions(func(set *pflag.FlagSet) {
				Expect(set.Set("api.disabled_query_parameters", "")).ToNot(HaveOccurred())
			})
			ctx = ctxBuilder.Build()
		})

		Context("the query param is provided", func() {
			It("Returns return an error", func() {
				ctx.SMWithOAuth.GET(web.ServicePlansURL).WithQuery("environment", "cf").
					Expect().
					Status(http.StatusOK)
			})
		})

	})

	Describe("query param is disabled", func() {
		AfterEach(func() {
			ctx.Cleanup()
		})

		BeforeEach(func() {
			ctxBuilder = common.NewTestContextBuilderWithSecurity().WithEnvPreExtensions(func(set *pflag.FlagSet) {
				Expect(set.Set("api.disabled_query_parameters", "environment")).ToNot(HaveOccurred())
			})
			ctx = ctxBuilder.Build()
		})

		Context("the query param is provided", func() {
			It("Returns return an error", func() {
				ctx.SMWithOAuth.GET(web.ServicePlansURL).WithQuery("environment", "cf").
					Expect().
					Status(http.StatusNotImplemented)
			})
		})
		Context("the query param is not provided", func() {
			It("Returns return an error", func() {
				ctx.SMWithOAuth.GET(web.ServicePlansURL).
					Expect().
					Status(http.StatusOK)
			})
		})

	})
})
