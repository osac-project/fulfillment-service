/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicv1

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestIdentityProviderTestResult_LogValue(t *testing.T) {
	message := "connection failed: bind DN cn=admin,dc=example,dc=com invalid password"

	result := &IdentityProviderTestResult{
		Success: false,
		Message: &message,
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("test completed", "result", result)

	logOutput := buf.String()

	// Verify sensitive message is NOT in logs
	if strings.Contains(logOutput, "bind DN") {
		t.Errorf("message with connection details should be redacted: %s", logOutput)
	}
	if strings.Contains(logOutput, "cn=admin") {
		t.Errorf("message with credentials should be redacted: %s", logOutput)
	}

	// Verify success status IS in logs
	if !strings.Contains(logOutput, "success") {
		t.Errorf("success field should be present: %s", logOutput)
	}
	if !strings.Contains(logOutput, "has_message") {
		t.Errorf("has_message indicator should be present: %s", logOutput)
	}
}

func TestIdentityProviderTestResult_LogValue_NilReceiver(t *testing.T) {
	var result *IdentityProviderTestResult // nil pointer

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Should not panic when logging a nil receiver
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("LogValue panicked with nil receiver: %v", r)
		}
	}()

	logger.Info("test completed", "result", result)

	logOutput := buf.String()

	// Verify no sensitive fields are present
	if strings.Contains(logOutput, "success") {
		t.Errorf("nil receiver should not log 'success' field: %s", logOutput)
	}
	if strings.Contains(logOutput, "has_message") {
		t.Errorf("nil receiver should not log 'has_message' field: %s", logOutput)
	}
}

func TestIdentityProviderHealth_LogValue(t *testing.T) {
	message := "LDAP connection to ldap://internal-server.example.com:389 failed"

	health := &IdentityProviderHealth{
		Status:      IdentityProviderHealthStatus_IDENTITY_PROVIDER_HEALTH_STATUS_UNHEALTHY,
		Message:     &message,
		LastChecked: timestamppb.New(time.Now()),
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("health check", "health", health)

	logOutput := buf.String()

	// Verify internal network details are NOT in logs
	if strings.Contains(logOutput, "internal-server") {
		t.Errorf("internal network details should be redacted: %s", logOutput)
	}

	// Verify status IS in logs
	if !strings.Contains(logOutput, "UNHEALTHY") {
		t.Errorf("status should be present: %s", logOutput)
	}
	if !strings.Contains(logOutput, "has_message") {
		t.Errorf("has_message indicator should be present: %s", logOutput)
	}
}

func TestIdentityProviderHealth_LogValue_NilReceiver(t *testing.T) {
	var health *IdentityProviderHealth // nil pointer

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Should not panic when logging a nil receiver
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("LogValue panicked with nil receiver: %v", r)
		}
	}()

	logger.Info("health check", "health", health)

	logOutput := buf.String()

	// Verify no sensitive fields are present
	if strings.Contains(logOutput, "UNHEALTHY") {
		t.Errorf("nil receiver should not log status field: %s", logOutput)
	}
	if strings.Contains(logOutput, "has_message") {
		t.Errorf("nil receiver should not log 'has_message' field: %s", logOutput)
	}
}
