# 3. UTILIZZO: Ora puoi creare le risorse (es. il tuo VIP/DNAT) senza logica imperativa
resource "fortios_firewall_vip" "<FGT_RESOURCE_NAME>" {
  name        = "<FGT_VIP_NAME>" 
  extip       = "<FGT_VIP_EXTIP>" 
  extintf     = "<FGT_VIP_EXTIF>" 
  mappedip {
    range = "<FGT_VIP_RANGE>" 
  }
}