/*
Copyright (c) 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package database

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Builder", func() {
	It("Should return an error if logger is not set", func() {
		_, err := NewContainer().Build()
		Expect(err).To(MatchError("logger is mandatory"))
	})
})

var _ = Describe("Instance", func() {
	It("Should create an instance and return a valid URL", func() {
		instance, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(instance.Close)

		url := instance.Url()
		Expect(url).To(ContainSubstring("postgres://"))
		Expect(url).To(ContainSubstring("sslmode=disable"))
	})

	It("Should create a working database handle", func() {
		instance, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(instance.Close)

		handle, err := instance.Handle()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(handle.Close)

		err = handle.Ping()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should close the instance without error", func() {
		instance, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())

		err = instance.Close()
		Expect(err).ToNot(HaveOccurred())
	})
})
