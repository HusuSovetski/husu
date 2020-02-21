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

package security_test

import (
	"context"

	"github.com/Peripli/service-manager/pkg/security"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type obj struct {
	integralData []byte
	integrity    [32]byte
}

func (o *obj) Encrypt(context.Context, func(context.Context, []byte) ([]byte, error)) error {
	return nil
}

func (o *obj) Decrypt(context.Context, func(context.Context, []byte) ([]byte, error)) error {
	return nil
}

func (o *obj) IntegralData() []byte {
	return o.integralData
}

func (o *obj) SetIntegrity(integrity [32]byte) {
	o.integrity = integrity
}

func (o *obj) GetIntegrity() [32]byte {
	return o.integrity
}

var _ = Describe("SHA256 processor", func() {
	var processor security.IntegrityProcessor
	var securedObject *obj
	var emptyIntegrity [32]byte

	BeforeEach(func() {
		processor = security.SHA256IntegrityProcessor()
		securedObject = &obj{
			integralData: []byte("integral data"),
		}
	})

	Context("Calculate", func() {
		Context("when the object is nil", func() {
			It("returns an error", func() {
				integrity, err := processor.CalculateIntegrity(nil)
				Expect(err).To(HaveOccurred())
				Expect(integrity).To(Equal(emptyIntegrity))
			})
		})

		Context("when the object has no integral data", func() {
			It("returns an error", func() {
				securedObject.integralData = []byte{}
				integrity, err := processor.CalculateIntegrity(securedObject)
				Expect(err).To(HaveOccurred())
				Expect(integrity).To(Equal(emptyIntegrity))
			})
		})

		Context("when the object has integral data", func() {
			It("returns calculated integrity", func() {
				integrity, err := processor.CalculateIntegrity(securedObject)
				Expect(integrity).To(Not(BeNil()))
				Expect(err).To(BeNil())
				Expect(integrity).To(Not(Equal(emptyIntegrity)))
			})
		})
	})

	Context("Validate", func() {
		Context("when the object is nil", func() {
			It("has valid integrity", func() {
				valid := processor.ValidateIntegrity(nil)
				Expect(valid).To(BeTrue())
			})
		})

		Context("when the object has no integral data", func() {
			It("has valid integrity", func() {
				securedObject.integralData = []byte{}
				valid := processor.ValidateIntegrity(securedObject)
				Expect(valid).To(BeTrue())
			})
		})

		Context("when the object has integral data", func() {
			It("validates integrity successfully", func() {
				integrity, err := processor.CalculateIntegrity(securedObject)
				Expect(err).ToNot(HaveOccurred())
				securedObject.SetIntegrity(integrity)
				valid := processor.ValidateIntegrity(securedObject)
				Expect(valid).To(BeTrue())
			})
		})
	})
})
