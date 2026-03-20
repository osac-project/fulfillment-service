/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConsoleComputeInstance(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Console ComputeInstance")
}

// Logger used for tests:
var logger *slog.Logger

var _ = BeforeSuite(func() {
	// Create a logger that writes to the Ginkgo writer, so that the log messages will be attached to the output of
	// the right test:
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewTextHandler(GinkgoWriter, options)
	logger = slog.New(handler)
})

var _ = Describe("Escape Detector", func() {
	var detector *escapeDetector

	BeforeEach(func() {
		detector = newEscapeDetector()
	})

	It("should detect CR ~ .", func() {
		Expect(detector.feed([]byte("\r"))).To(BeFalse())
		Expect(detector.feed([]byte("~"))).To(BeFalse())
		Expect(detector.feed([]byte("."))).To(BeTrue())
	})

	It("should detect LF ~ .", func() {
		Expect(detector.feed([]byte("\n"))).To(BeFalse())
		Expect(detector.feed([]byte("~"))).To(BeFalse())
		Expect(detector.feed([]byte("."))).To(BeTrue())
	})

	It("should detect the sequence in a single chunk", func() {
		Expect(detector.feed([]byte("\r~."))).To(BeTrue())
	})

	It("should not trigger on ~ . without CR", func() {
		Expect(detector.feed([]byte("~."))).To(BeFalse())
	})

	It("should not trigger on CR ~ without .", func() {
		Expect(detector.feed([]byte("\r~x"))).To(BeFalse())
	})

	It("should reset after failed sequence", func() {
		Expect(detector.feed([]byte("\r~x"))).To(BeFalse())
		Expect(detector.feed([]byte("\r~."))).To(BeTrue())
	})

	It("should handle multiple CRs", func() {
		Expect(detector.feed([]byte("\r\r\r~."))).To(BeTrue())
	})

	It("should not trigger on normal text", func() {
		Expect(detector.feed([]byte("hello world"))).To(BeFalse())
	})

	It("should handle embedded sequence in larger input", func() {
		Expect(detector.feed([]byte("hello\r~."))).To(BeTrue())
	})

	It("should detect Ctrl+] without preceding Enter", func() {
		Expect(detector.feed([]byte{0x1D})).To(BeTrue())
	})

	It("should detect Ctrl+] embedded in input", func() {
		Expect(detector.feed([]byte("typing something\x1D"))).To(BeTrue())
	})
})
