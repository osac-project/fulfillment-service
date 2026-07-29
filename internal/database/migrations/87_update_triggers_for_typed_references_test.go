/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package migrations

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = DescribeMigration("Update triggers for typed references", func() {
	It("Backfills string reference fields to typed reference objects", func(ctx context.Context) {
		// Create the parent virtual network so the Z0002 forward-reference trigger passes
		_, err := conn.Exec(ctx,
			`insert into virtual_networks (id, tenant, data) values ($1, $2, $3)`,
			"vn-old-string", "system", `{"spec":{"ipv4_cidr":"10.0.0.0/16"}}`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Insert a subnet with old-style string virtual_network reference
		_, err = conn.Exec(ctx,
			`insert into subnets (id, tenant, data) values ($1, $2, $3)`,
			"subnet-1", "system",
			`{"spec":{"virtual_network":"vn-old-string","ipv4_cidr":"10.0.0.0/24"}}`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Apply migration 87
		err = tool.Migrate(ctx, 87)
		Expect(err).ToNot(HaveOccurred())

		// Verify the string was converted to a typed reference object
		var data json.RawMessage
		err = conn.QueryRow(ctx,
			`select data from subnets where id = $1`, "subnet-1",
		).Scan(&data)
		Expect(err).ToNot(HaveOccurred())

		var parsed map[string]interface{}
		err = json.Unmarshal(data, &parsed)
		Expect(err).ToNot(HaveOccurred())

		spec := parsed["spec"].(map[string]interface{})
		vn := spec["virtual_network"]
		vnObj, ok := vn.(map[string]interface{})
		Expect(ok).To(BeTrue(), "virtual_network should be a JSON object after backfill")
		Expect(vnObj["id"]).To(Equal("vn-old-string"))
	})

	It("Skips rows already in the new format", func(ctx context.Context) {
		// Disable the Z0002 trigger so we can insert directly with the new format
		_, err := conn.Exec(ctx,
			`alter table subnets disable trigger check_subnet_virtual_network_ref`,
		)
		Expect(err).ToNot(HaveOccurred())

		// Insert a subnet already using the typed reference format
		_, err = conn.Exec(ctx,
			`insert into subnets (id, tenant, data) values ($1, $2, $3)`,
			"subnet-2", "system",
			`{"spec":{"virtual_network":{"id":"vn-id","name":"vn-name"},"ipv4_cidr":"10.0.1.0/24"}}`,
		)
		Expect(err).ToNot(HaveOccurred())

		err = tool.Migrate(ctx, 87)
		Expect(err).ToNot(HaveOccurred())

		var data json.RawMessage
		err = conn.QueryRow(ctx,
			`select data from subnets where id = $1`, "subnet-2",
		).Scan(&data)
		Expect(err).ToNot(HaveOccurred())

		var parsed map[string]interface{}
		err = json.Unmarshal(data, &parsed)
		Expect(err).ToNot(HaveOccurred())

		spec := parsed["spec"].(map[string]interface{})
		vnObj := spec["virtual_network"].(map[string]interface{})
		Expect(vnObj["id"]).To(Equal("vn-id"))
		Expect(vnObj["name"]).To(Equal("vn-name"))
	})
})
