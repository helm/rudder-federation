# Federation rudder (helm release plugin) which enables federation support for your Helm charts.

## Usage
You need two things - Tiller with `--experimental-release` flag enabled, and this rudder available to Tiller. The easiest way to do it is to add container with federated rudder to Tiller deployment - check out [ruddered-tiller.yaml](manifests/ruddered-tiller.yaml).


