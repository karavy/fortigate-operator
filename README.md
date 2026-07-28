# DISCLAIMER OF WARRANTY & LIMITATION OF LIABILITY

THIS SOFTWARE IS PROVIDED "AS IS" AND IS INTENDED SOLELY FOR EXPERIMENTAL, EDUCATIONAL, AND LABORATORY TESTING PURPOSES. It has not undergone production-grade security auditing, performance optimization, or comprehensive vulnerability screening.

UNDER NO CIRCUMSTANCES SHOULD THIS SOFTWARE BE USED IN PRODUCTION ENVIRONMENTS, LIVE INFRASTRUCTURES, OR MISSION-CRITICAL SYSTEMS WHERE NETWORK SECURITY, DATA INTEGRITY, OR SERVICE AVAILABILITY ARE REQUIRED.

THE AUTHOR(S) AND CONTRIBUTORS ACCEPTS NO LIABILITY WHATSOEVER FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, NETWORK BREACHES, FIREWALL FAILURES, UNAUTHORIZED ACCESS, LOSS OF DATA, OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE. 

USE ENTIRELY AT YOUR OWN RISK.

# k8s-operator-fortigate
Un operatore per la gestione di firewall Fortigate su kubernetes. L'operator è pensato per creare le vm che ospitano il firewall utilizzando Kubevirt

## Scelta di terraform

Viene utilizzato Terraform perchè è la tecnologia più aggiornata ed affidabile per l'automazione della gestione dei firewall Fortigate. 

## Gestione dei template

[Gestione Template](docs/terraform-rules/README.md)

## Documentazione per installazione ed esempi

[Installazione](docs/requirements/README.md)

## Struttura del bucket S3
Il bucket S3 deve avere una struttura come quella indicata sotto:

```
fortigate-operator-bucket/
├── firmwares/
    ├── vX.Y.Z/
    |   └── FGT_VM64_KVM-vX.Y.Z-buildXXXX-FORTINET.out
    └── vX.Y.Q/
        └── FGT_VM64_KVM-vX.Y.Q-buildXXXX-FORTINET.out
├── upgrade-backup/
└── rules-templates/
```

- nel path firmwares vanno inserite le immagini per l'upgrade dei firewall. 
- upgrade-backup conterrà i backup dei firewall effettuati durante l'upgrade. 
- rules-templates deve contenere i template dei terraform utilizzati dall'operator 