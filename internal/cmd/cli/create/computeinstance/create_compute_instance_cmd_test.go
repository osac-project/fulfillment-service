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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

var _ = Describe("parseNetworkAttachmentFlag", func() {
	DescribeTable("when input is valid it should parse",
		func(input, wantSubnet string, wantSGs []string) {
			got, err := parseNetworkAttachmentFlag(input)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.GetSubnet()).To(Equal(wantSubnet))
			Expect(got.GetSecurityGroups()).To(Equal(wantSGs))
		},
		Entry("when value is bare subnet id it should use it as subnet",
			"  sub-1  ", "sub-1", nil),
		Entry("when value is subnet=key form it should parse subnet",
			"subnet=sub-2", "sub-2", nil),
		Entry("when value includes security_groups alias it should parse groups",
			"subnet=a,security_groups=g1,g2", "a", []string{"g1", "g2"}),
		Entry("when value lists security-groups after subnet it should parse groups",
			"subnet=b,security-groups=x", "b", []string{"x"}),
	)

	DescribeTable("when input is invalid it should error",
		func(input string) {
			_, err := parseNetworkAttachmentFlag(input)
			Expect(err).To(HaveOccurred())
		},
		Entry("when value is empty it should error", "   "),
		Entry("when key is unknown it should error", "subnet=a,foo=bar"),
		Entry("when subnet= form omits subnet it should error", "security-groups=g1"),
		Entry("when fragment has no equals it should error", "subnet=sub,noglue"),
		Entry("when value after equals is empty it should error", "subnet="),
		Entry("when subnet is duplicated it should error", "subnet=a,subnet=b"),
	)
})

var _ = Describe("applyNetworkingFlags", func() {
	It("should error when legacy subnet and network-attachments are combined", func() {
		c := &runnerContext{}
		c.args.subnet = "sub-1"
		c.args.networkAttachments = []string{"other"}
		var b publicv1.ComputeInstanceSpec_builder
		err := c.applyNetworkingFlags(&b)
		Expect(err).To(HaveOccurred())
	})

	It("should error when legacy security-groups and network-attachments are combined", func() {
		c := &runnerContext{}
		c.args.securityGroups = []string{"sg-1"}
		c.args.networkAttachments = []string{"sub-x"}
		var b publicv1.ComputeInstanceSpec_builder
		err := c.applyNetworkingFlags(&b)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("buildSpec", func() {
	It("should populate spec when legacy networking flags are set", func() {
		c := &runnerContext{}
		c.args.subnet = "legacy-sub"
		c.args.securityGroups = []string{"sg-a", "sg-b"}
		spec, err := c.buildSpec("tmpl", nil)
		Expect(err).NotTo(HaveOccurred())

		want := publicv1.ComputeInstanceSpec_builder{
			Template:       "tmpl",
			Subnet:         proto.String("legacy-sub"),
			SecurityGroups: []string{"sg-a", "sg-b"},
		}.Build()
		Expect(proto.Equal(spec, want)).To(BeTrue(), "spec should equal expected spec")
	})

	It("should populate attachments when network-attachment flags are set", func() {
		c := &runnerContext{}
		c.args.networkAttachments = []string{"n1", "subnet=n2,security-groups=g1"}
		spec, err := c.buildSpec("tmpl", nil)
		Expect(err).NotTo(HaveOccurred())

		want := publicv1.ComputeInstanceSpec_builder{
			Template: "tmpl",
			NetworkAttachments: []*publicv1.NetworkAttachment{
				publicv1.NetworkAttachment_builder{Subnet: "n1"}.Build(),
				publicv1.NetworkAttachment_builder{Subnet: "n2", SecurityGroups: []string{"g1"}}.Build(),
			},
		}.Build()
		Expect(proto.Equal(spec, want)).To(BeTrue(), "spec should equal expected spec")
	})
})

var _ = Describe("Create computeinstance flag registration", func() {
	It("should register --catalog-item flag", func() {
		cmd := Cmd()
		flag := cmd.Flags().Lookup("catalog-item")
		Expect(flag).NotTo(BeNil())
		Expect(flag.Usage).To(ContainSubstring("Catalog item"))
	})

	It("should still register --template flag", func() {
		cmd := Cmd()
		flag := cmd.Flags().Lookup("template")
		Expect(flag).NotTo(BeNil())
	})

	It("should register --catalog-item without a short flag", func() {
		cmd := Cmd()
		flag := cmd.Flags().Lookup("catalog-item")
		Expect(flag).NotTo(BeNil())
		Expect(flag.Shorthand).To(BeEmpty())
	})

	It("should keep -t as shorthand for --template", func() {
		cmd := Cmd()
		flag := cmd.Flags().Lookup("template")
		Expect(flag).NotTo(BeNil())
		Expect(flag.Shorthand).To(Equal("t"))
	})
})

var _ = Describe("Create computeinstance flag validation", func() {
	It("should return error when both --catalog-item and --template are set", func() {
		cmd := Cmd()
		cmd.SetArgs([]string{"--catalog-item", "cat-001", "--template", "tpl-001", "--name", "test"})
		err := cmd.Execute()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("if any flags in the group"))
		Expect(err.Error()).To(ContainSubstring("catalog-item"))
		Expect(err.Error()).To(ContainSubstring("template"))
	})

	It("should return error when neither --catalog-item nor --template is set", func() {
		cmd := Cmd()
		cmd.SetArgs([]string{"--name", "test"})
		err := cmd.Execute()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("at least one of the flags"))
		Expect(err.Error()).To(ContainSubstring("catalog-item"))
		Expect(err.Error()).To(ContainSubstring("template"))
	})
})
