# Installazione kind

Installa kind e crea un cluster

# Configurazione nodo kind

### Sul nodo kind
docker exec -it kind-control-plane bash
ip link add br-ext type bridge
ip link set br-ext up
ip addr add 192.168.99.254/24 dev br-ext

### Sul pc che ospita Docker
sudo ip route add 192.168.99.0/24 via 172.19.0.2 (rete docker di kind)

# Installazione Minio

docker run -d --network kind -p 9000:9000 -p 9001:9001 quay.io/minio/minio server /data --console-address ":9001"

mc alias set mio-minio http://localhost:9000 minioadmin minioadmin
mc admin user svcacct add mio-minio minioadmin

# Networking
Se utilizzo minio in un docker, avviare nella rete di kind (bridge)

Se creo una vm per i pvc (truenas) configurare la rete come bridge e inserire nella rte kind

# Attivazione account trial per Fortigate
La registrazione della licenza richiede un account Fortigate per la trial. In questo modo sarà possibile attivare la licenza sul firewall di test

# Installazione e configurazione del servizio Load Balancer

kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.16.1/config/manifests/metallb-native.yaml
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

# Upload dell'immagine Fortigate e del disco cloud init

/bin/virtctl image-upload pvc fortios-v766m-build3652-fgt --size 5Gi --uploadproxy-url https://172.19.37.50 --insecure --image-path /tmp/fortios.qcow2

/bin/virtctl image-upload pvc fortios-v766m-build3652-cloud-init --size 1Gi --uploadproxy-url https://172.19.37.50 --insecure --image-path /tmp/fgt-bootstrap.iso 

# Installazione CSI

curl -skSL https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/v4.13.2/deploy/install-driver.sh | bash -s v4.13.2 --

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