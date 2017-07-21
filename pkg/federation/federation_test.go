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
	"testing"
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
