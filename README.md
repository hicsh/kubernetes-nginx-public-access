# kubernetes-nginx-public-access

### Prerequisities
- Pulumi: https://www.pulumi.com/docs/iac/download-install/
- Golang: https://go.dev/doc/install
- Openssl: https://openssl-library.org/source/
- Helm: https://helm.sh/docs/intro/install/
- Curl: https://curl.se/download.html
- Azure cli: https://learn.microsoft.com/en-us/cli/azure/install-azure-cli

### Log in to the Pulumi Cloud
We can use --local parameter to save state information locally.

```
export PULUMI_CONFIG_PASSPHRASE=""
pulumi login --local
```

### Authorize to Azure
We can use `az login` from the terminal for the PoC purpose.

### Select existing stack
```
pulumi stack select poc --cwd infrastructure
```

### Create the infrastructure required for PoC
```
export ARM_SUBSCRIPTION_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
pulumi up --cwd infrastructure
```

### Helm deployment of Ingress-nginx
```
pulumi stack output kubeConfig --show-secrets --cwd infrastructure > kubeconfig
export KUBECONFIG=kubeconfig

helm dependency build ./ingress-nginx
helm upgrade --kubeconfig=kubeconfig --install --namespace ingress-nginx --create-namespace ingress ./ingress-nginx
```

### Cleanup the infrastructure
```
pulumi destroy --cwd infrastructure
```



### Access
- VM1:
    ```
    pulumi stack output vm1-privatekey --cwd infrastructure --show-secrets > vm1.rsa
    chmod 600 vm1.rsa
    ssh -i vm1.rsa pulumiuser@$(pulumi stack output vm1-ip --cwd infrastructure)
    ```
- VM2:
    ```
    pulumi stack output vm2-privatekey --cwd infrastructure --show-secrets > vm2.rsa
    chmod 600 vm2.rsa
    ssh -i vm2.rsa pulumiuser@$(pulumi stack output vm2-ip --cwd infrastructure)
    ```

### Certificates preparaton
- CA:
    ```
    openssl genrsa -out ca.key 4096
    openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -out ca.crt
    kubectl create secret generic mtls-ca-cert --from-file ca.crt --dry-run=client -o yaml > ingress-nginx/templates/mtls-ca-cert-secret.yaml
    ```
- client
    ```
    openssl genpkey -algorithm RSA -out client.key -pkeyopt rsa_keygen_bits:2048
    openssl req -new -key client.key -out client.csr -subj "/C=US/ST=California/L=SanFrancisco/O=MyOrganization/OU=MyOrgUnit/CN=client.example.com"
    openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 365
    ```

### Test
```
curl -k --cert client.crt --key client.key --cacert ca.crt https://51.138.4.141.nip.io
curl -k https://51.138.4.141.nip.io

scp -i vm2.rsa {client.crt,client.key,ca.crt} pulumiuser@$(pulumi stack output vm2-ip --cwd infrastructure):/tmp/
curl -k --cert /tmp/client.crt --key /tmp/client.key --cacert /tmp/ca.crt https://51.138.4.141.nip.io
```

### Get fingerprint sh1 verified by ingress by default
```
openssl x509 -in client.crt -noout -fingerprint -sha1 | awk -F= '{print $2}' | sed 's/://g'
```


