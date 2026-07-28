<FGT_FOR_STR><FIREWALLRULES>
resource "fortios_firewall_policy" "<FIREWALLRULES_RULENAME>" {
  action                      = "accept"
  anti_replay                 = "enable"
  auth_path                   = "disable"
  auto_asic_offload           = "enable"
  #av_profile                  = "wifi-default"
  inspection_mode             = "flow"
  internet_service            = "enable"
  #ips_sensor                  = "protect_email_server"
  logtraffic                  = "all"
  name                        = "<FIREWALLRULES_RULENAME>"
  schedule                    = "always"
  #ssl_ssh_profile             = "certificate-inspection"
  status                      = "enable"
  #utm_status                  = "enable"

  dstintf {
      name = "port1"
  }
  
  <FGT_FOR_STR><INTERNETSERVICES>
  internet_service_name {
      name = "<FIREWALLRULES_INTERNETSERVICES_NAME>"
  }
  <FGT_FOR_END>

  srcaddr {
      name = "FABRIC_DEVICE"
  }

  srcintf {
      name = "port2"
  }
}
<FGT_FOR_END>