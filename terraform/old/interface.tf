resource "fortios_system_interface" "<FGT_RESOURCE_NAME>" {
  algorithm    = "L4"
  defaultgw    = "enable"
  distance     = 5
  ip           = "<FGT_PORT_ADDRESS> <FGT_PORT_MASK>"
  mtu          = 1500
  mtu_override = "disable"
  name         = "<FGT_PORT_NAME>"
  type         = "physical"
  vdom         = "root"
  mode         = "static"
  status       = "up"
  description  = "Created by Terraform Provider for FortiOS"
  allowaccess  = "ping https"
  ipv6 {
    nd_mode = "basic"
  }
}