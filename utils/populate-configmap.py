import yaml
import base64
from os import path

config_path  = path.join(path.expanduser("~"), ".kube", "config")

with open(config_path) as f:
    a = f.read()

data = yaml.load(a)

config_map = {
    "apiVersion": "v1",
    "kind": "ConfigMap",
    "metadata": {
        "name": "federation-credentials",
        "namespace": "kube-system",
    },
    "data": {
        "type": "tls",
        "cadata": base64.b64decode([cluster for cluster in data["clusters"] if cluster["name"] == "federation"][0]['cluster']['certificate-authority-data'])[:-1],
        "certdata":base64.b64decode([user for user in data["users"] if user["name"] == "federation"][0]["user"]["client-certificate-data"])[:-1],
        "keydata":base64.b64decode([user for user in data["users"] if user["name"] == "federation"][0]["user"]["client-key-data"])[:-1],
        "host": "https://federation-apiserver.federation-system:443"
    }
}

print yaml.dump(config_map, default_flow_style=False, default_style='"')
