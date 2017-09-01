/*
Copyright 2017 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package federation

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/apis/extensions"
)

func TestSplitManifestForFed(t *testing.T) {

	manifest := `---
apiVersion: v1
kind: Secret
metadata:
  name: wp4-mariadb
type: Opaque
data:
  mariadb-root-password: ""
  mariadb-password: ""
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: wp4-mariadb
data:
  my.cnf: "trolo"
---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: wp4-wordpress
spec:
  accessModes:
    - "ReadWriteOnce"
  resources:
    requests:
      storage: "10Gi"
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: wp4-wordpress
spec:
  replicas: 1
---`

	expectedFederated := `---
apiVersion: v1
kind: Secret
metadata:
  name: wp4-mariadb
type: Opaque
data:
  mariadb-root-password: ""
  mariadb-password: ""
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: wp4-mariadb
data:
  my.cnf: "trolo"
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: wp4-wordpress
spec:
  replicas: 1
---`

	expectedLocal := `---
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: wp4-wordpress
spec:
  accessModes:
    - "ReadWriteOnce"
  resources:
    requests:
      storage: "10Gi"
---`

	federated, local, err := SplitManifestForFed(manifest)

	if err != nil {
		t.Errorf("error not nil, got %v", err)
	}

	if federated != expectedFederated {
		t.Errorf("federated other than expected. expected:\n%v\ngot:\n%v", expectedFederated, federated)
	}

	if local != expectedLocal {
		t.Errorf("local other than expected. expected:\n%v\ngot:\n%v", expectedLocal, local)
	}
}

var manifest = `---
apiVersion: v1
kind: Secret
metadata:
  name: wp4-mariadb
type: Opaque
data:
  mariadb-root-password: ""
  mariadb-password: ""
---
`

var deployment = extensions.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{
			"federation.alpha.kubernetes.io/federation-name": "federation",
			"federations":                                    "federation=example.com",
		},
	},
}

func TestReplaceWithFederationDeploymentStaySame(t *testing.T) {
	replacements := []Replace{
		Replace{
			From: "non-existent",
			To:   "non-existen",
		},
	}
	replaced, err := ReplaceWithFederationDeployment(manifest, replacements, &deployment)

	if err != nil {
		t.Fatalf("Expected no errors, got %v", err)
	}

	if replaced != manifest {
		t.Fatalf("Expected replaced to be the same as manifest")
	}
}

func TestReplaceWithFederationDeploymentSimpleReplace(t *testing.T) {
	replacements := []Replace{
		Replace{
			From: `mariadb-root-password: ""`,
			To:   `mariadb-root-password: newer-root-password`,
		},
	}
	replaced, err := ReplaceWithFederationDeployment(manifest, replacements, &deployment)

	if err != nil {
		t.Fatalf("Expected no errors, got %v", err)
	}

	if replaced != strings.Replace(manifest, `mariadb-root-password: ""`, `mariadb-root-password: newer-root-password`, 1) {
		t.Fatalf("Replacement not as expected")
	}
}

func TestReplaceWithFederationDeploymentTemplateReplace(t *testing.T) {
	replacements := []Replace{
		Replace{
			From: `mariadb-root-password: ""`,
			To:   `mariadb-root-password: {{ index .ObjectMeta.Annotations "federations" }}`,
		},
	}
	replaced, err := ReplaceWithFederationDeployment(manifest, replacements, &deployment)

	if err != nil {
		t.Fatalf("Expected no errors, got %v", err)
	}

	if replaced != strings.Replace(manifest, `mariadb-root-password: ""`, `mariadb-root-password: federation=example.com`, 1) {
		t.Logf("manifest: %s\nreplaced: %s\n", manifest, replaced)
		t.Fatalf("Replacement not as expected")
	}
}

func TestReplaceWithFederationDeploymentMultilineTemplateReplace(t *testing.T) {
	replacements := []Replace{
		Replace{
			From: `type: Opaque
data:
  mariadb-root-password: ""
  mariadb-password: ""`,
			To: `type: Opaque
data:
  mariadb-root-password: {{ index .ObjectMeta.Annotations "federations" }}
  mariadb-password: {{ index .ObjectMeta.Annotations "federations" }}`,
		},
	}
	replaced, err := ReplaceWithFederationDeployment(manifest, replacements, &deployment)

	if err != nil {
		t.Fatalf("Expected no errors, got %v", err)
	}

	if replaced != strings.Replace(manifest, `""`, `federation=example.com`, 2) {
		t.Logf("manifest: %s\nreplaced: %s\n", manifest, replaced)
		t.Fatalf("Replacement not as expected")
	}
}

func TestReplaceWithFederationDeploymentRegexFrom(t *testing.T) {
	replacements := []Replace{
		Replace{
			From: `mariadb-.*\n`,
			To: `{{ index .ObjectMeta.Annotations "federations" }}
`,
		},
	}
	replaced, err := ReplaceWithFederationDeployment(manifest, replacements, &deployment)

	if err != nil {
		t.Fatalf("Expected no errors, got %v", err)
	}
	expected := `---
apiVersion: v1
kind: Secret
metadata:
  name: wp4-mariadb
type: Opaque
data:
  federation=example.com
  federation=example.com
---
`

	if replaced != expected {
		t.Logf("expected: %s\nreplaced: %s\n", expected, replaced)
		t.Fatalf("Replacement not as expected")
	}
}
