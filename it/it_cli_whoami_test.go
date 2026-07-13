/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI Whoami", Label("cli", "whoami"), func() {
	var homeDir string

	BeforeEach(func() {
		var err error
		homeDir, err = tool.NewCLIHomeDir()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := os.RemoveAll(homeDir)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("Displays current user after login", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "whoami")
		Expect(exitCode).To(Equal(0), "whoami should succeed after login")
		Expect(stdout).To(ContainSubstring(adminUsername))
	})

	It("Fails without login", func(ctx context.Context) {
		_, stderr, exitCode := tool.RunCLI(ctx, homeDir, "whoami")
		Expect(exitCode).ToNot(Equal(0), "whoami without login should fail")

		combinedOutput := stderr
		Expect(combinedOutput).To(SatisfyAny(
			ContainSubstring("Not logged in"),
			ContainSubstring("there is no configuration"),
		))
	})
})
