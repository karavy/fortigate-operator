# Installazione kind

Installa kind e crea un cluster

# Configurazione nodo kind

# Creazione immagine

# Rete Multus 
### Sul nodo kind
docker exec -it kind-control-plane bash
ip link add br-ext type bridge
ip link set br-ext up
ip addr add 192.168.99.254/24 dev br-ext

sysctl -w net.ipv4.ip_forward=1
iptables -I FORWARD -i br-ext -j ACCEPT
iptables -I FORWARD -o br-ext -j ACCEPT

### IP per VLAN sulle NAD (bridge creato dall'operator/NodeBridge)
Il bridge (es. br-ext, o il nome impostato in spec.bridgeName della NodeBridge) viene creato
VLAN-aware dall'operator. Per ogni VLAN referenziata dalle NetworkAttachmentDefinition (campo
`vlanID` delle porte) va creata sul nodo una sub-interfaccia 802.1q sul bridge, con il proprio
indirizzo di gateway. Ripetere per ogni <VLAN_ID>/<IP> usati dalle NAD:

bridge vlan add dev <BRIDGE_NAME> vid <VLAN_ID> self
ip link add link <BRIDGE_NAME> name vlan<VLAN_ID> type vlan id <VLAN_ID>
ip link set vlan<VLAN_ID> up
ip addr add <IP>/<PREFIX> dev vlan<VLAN_ID>

NOTA: i nomi interfaccia Linux sono limitati a 15 caratteri (IFNAMSIZ). Se il bridge ha un nome
già lungo (es. `br-91f65f7372cd`, tipico dei bridge auto-generati da Docker), NON concatenare
`<BRIDGE_NAME>.<VLAN_ID>` come nome della sub-interfaccia: si supera il limite e `ip link add`
fallisce con "not a valid ifname". Usare invece un nome breve indipendente (es. `vlan<VLAN_ID>`
come sopra): il parametro `link <BRIDGE_NAME>` è sufficiente ad associarla al bridge corretto.

Esempio con due VLAN (10 e 20):

bridge vlan add dev br-ext vid 10 self
ip link add link br-ext name vlan10 type vlan id 10
ip link set vlan10 up
ip addr add 192.168.10.254/24 dev vlan10

bridge vlan add dev br-ext vid 20 self
ip link add link br-ext name vlan20 type vlan id 20
ip link set vlan20 up
ip addr add 192.168.20.254/24 dev vlan20

### Sul pc che ospita Docker
sudo ip route add 192.168.99.0/24 via <ip container docker su cui gira kind>

# Installazione cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.21.1/cert-manager.yaml

# Installazione Minio

docker run -d --network kind -p 9000:9000 -p 9001:9001 quay.io/minio/minio server /data --console-address ":9001"

mc alias set mio-minio http://localhost:9000 minioadmin minioadmin
mc admin user svcacct add mio-minio minioadmin

# Installazione kubevirt
export RELEASE=$(curl https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-operator.yaml
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-cr.yaml
kubectl -n kubevirt wait kv kubevirt --for condition=Available

# Networking
Se utilizzo minio in un docker, avviare nella rete di kind (bridge)

Se creo una vm per i pvc (truenas) configurare la rete come bridge e inserire nella rete kind

# Attivazione account trial per Fortigate
La registrazione della licenza richiede un account Fortigate per la trial. In questo modo sarà possibile attivare la licenza sul firewall di test

# Installazione e configurazione del servizio Load Balancer

kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.16.1/config/manifests/metallb-native.yaml
sleep 10
kubectl apply -f docs/requirements/metallb.yaml

# Creazione disco di boot cloud-init

genisoimage -o /tmp/fgt-bootstrap.iso -R -J docs/cloud-init/cdrom

# Installazione disk importer (CDI)

export TAG=$(curl -s -w %{redirect_url} https://github.com/kubevirt/containerized-data-importer/releases/latest)
export VERSION=$(echo ${TAG##*/})
kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/$VERSION/cdi-operator.yaml
kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/$VERSION/cdi-cr.yaml

kubectl apply -f docs/requirements/cdi-external-svc.yaml

# Installazione virtctl

export VERSION=$(curl https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)
wget https://github.com/kubevirt/kubevirt/releases/download/${VERSION}/virtctl-${VERSION}-linux-amd64
sudo mv virtctl-${VERSION}-linux-amd64 /bin/virtctl
chmod +x /bin/virtctl

# Installazione CSI

curl -skSL https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/v4.13.2/deploy/install-driver.sh | bash -s v4.13.2 --

# Upload dell'immagine Fortigate e del disco cloud init

/bin/virtctl image-upload pvc fortios-v766m-build3652-fgt --uploadproxy-url https://<IP_SERVIZIO_cdi-uploadproxy-ext_cdi> --size 5Gi --insecure --image-path /tmp/fortios.out

/bin/virtctl image-upload pvc fortios-v766m-build3652-cloud-init  --size 1Gi  --insecure --image-path /tmp/fgt-bootstrap.iso --uploadproxy-url https://<IP_SERVIZIO_cdi-uploadproxy-ext_cdi>

# Upload dell'immagine VyOS e del disco cloud init

/bin/virtctl image-upload pvc vyos-2026.03-golden --uploadproxy-url https://<IP_SERVIZIO_cdi-uploadproxy-ext_cdi> --size 10Gi --insecure --image-path /tmp/vyos-2026.03-golden.qcow2

# Installazione Multus

kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml

## Installazione su kind

kubectl apply -f docs/requirements/cni-plugins.yaml

# Creazione secret

## AWS credential (S3)
```
apiVersion: v1
kind: Secret
metadata:
  name: aws-credentials
type: Opaque
stringData:
  accessKeyID: 
  secretAccessKey: 
  s3Url:
```

## Fortigate credential
```
apiVersion: v1
kind: Secret
metadata:
  name: forti-credentials
type: Opaque
stringData:
  apiUserName: "admin-api-kubevirt"
  adminPassword: 
```

## Fortigate portal credential
```
apiVersion: v1
kind: Secret
metadata:
  name: forti-portal
type: Opaque
stringData:
  accountID: ""
  accountPassword: ""
```

## SSH firewall key
```
apiVersion: v1
kind: Secret
metadata:
  name: ssh-key
data:
  id_rsa: 

```

# Creazione rete per schede aggiuntive

kubectl apply -f docs/requirements/multus.yaml

# Abilitare le snapshot in kubevirt

kubectl apply -f docs/requirements/kubevirt-snapshot.yaml

# Creare il pvc che contiene i firmware per l'aggiornamento

kubectl apply -f docs/requirements/firmware-pvc.yaml
TODO: Copiare i firmware nel pvc

# Avviare un server S3

docker run -p 9000:9000 -p 9001:9001 quay.io/minio/minio server /data --console-address ":9001"

# Installazione snapshot controller

kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml

kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/master/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml

# TODO: Installazione Alloy per metriche

Installare alloy per recuperare i log dai fortigate

### values.yaml
```
alloy:
  configMap:
    create: true
    # Questa è la chiave corretta per inserire la configurazione River
    content: |
      // Configurazione per ascoltare il Syslog sulla porta 1514
      loki.source.syslog "ascolto_syslog" {
        listener {
          address       = "0.0.0.0:1514"
          protocol      = "udp"
          syslog_format = "rfc5424"
        }
        forward_to = [loki.echo.stampa_a_video.receiver]
      }

      // Componente che stampa i log ricevuti nello standard output (a video)
      loki.echo "stampa_a_video" {}

  # Ricordati di esporre la porta UDP anche a livello di Pod/Service di Kubernetes
  extraPorts:
    - name: syslog-udp
      port: 1514
      targetPort: 1514
      protocol: UDP
```

### installazione
helm upgrade --install alloy grafana/alloy -n forti-alloy --create-namespace  -f values.yaml