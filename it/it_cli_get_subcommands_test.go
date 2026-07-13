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
	"encoding/json"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI Get Subcommands", Label("cli", "get"), func() {
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

	It("Get token returns a JWT", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "token")
		Expect(exitCode).To(Equal(0), "get token should succeed")
		Expect(stdout).ToNot(BeEmpty(), "token should not be empty")

		// A JWT has three base64-encoded segments separated by dots.
		token := strings.TrimSpace(stdout)
		parts := strings.Split(token, ".")
		Expect(parts).To(HaveLen(3), "token should be a valid JWT with 3 parts")
	})

	It("Get token --header returns valid JSON", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "token", "--header")
		Expect(exitCode).To(Equal(0), "get token --header should succeed")
		Expect(stdout).ToNot(BeEmpty())

		var parsed map[string]any
		err := json.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred(), "token header should be valid JSON")
		Expect(parsed).To(HaveKey("alg"), "JWT header should contain 'alg' field")
	})

	It("Get publicippool lists or shows empty", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "publicippool")
		Expect(exitCode).To(Equal(0), "get publicippool should succeed")
		// May be empty or contain results — just verify no crash and valid output.
		Expect(stdout).ToNot(ContainSubstring("runtime error"))
	})
})
