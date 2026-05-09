storage "raft" {
  path    = "/bao/data"
  node_id = "bao-compose-1"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true
}

api_addr     = "http://bao:8200"
cluster_addr = "http://bao:8201"

ui           = true
disable_mlock = true
