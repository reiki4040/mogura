# Mogura

Mogura is the proxy that connect to microservices over network via SSH port forwarding.
currently, support MacOS only, and alpha developing status.

## use case

You are developing microservice that need to connect to some other microservices.
However, all other microservices can not work on local reality. Then connect to services that in development environment via VPN or SSH port forwarding.

Get a problem. it is hard to get other services and write/start port forwarding in start development in every day.

Mogura creates connections to microservices port forwarding.

```
[local] -(ssh)-> [bastion] -(port forward)-> [target(ex microservice)]
```

## install

install with homebrew

```
brew install reiki4040/tap/mogura
```

upgrade

```
brew update
brew upgrade mogura
```

## settings

default load config file path is `~/.mogura/config.yml`. if you want specify other file, then use `-config` option.

### sample settings

example for EC2 or RDS etc...

```
bastion_ssh_config:
  host: your.bastion.example.com
  user: ec2-user
  # port: 22
  #key_path: ~/ssh_key.pem
tunnels:
  - name: nginx-on-ec2
    local_bind_port: 8080
    target: nginx.your.private.domain
    target_port: 80
    forwarding_timeout: 5s # duration format. if not defined then default timeout 5s
  - name: rds-mysql
    local_bind_port: 3306
    target: db.your.private.domain
    target_port: 3306
    forwarding_timeout: 5m
```

example for ECS that set service discovery to SRV record
```
bastion_ssh_config:
  host: your.bastion.example.com
  user: ec2-user
  # port: 22
  #key_path: ~/ssh_key.pem
  remote_dns: 10.0.0.2:53
tunnels:
  - name: ecs-service1
    local_bind_port: 8080
    target: service1.your.private.domain
    target_type: "SRV"
  - name: ecs-service2
    local_bind_port: 8081
    target: service2.your.private.domain
    target_type: "SRV"
```

if you use ssh private key with passphrase, then use `MOGURA_PASSPHRASE` environment variable.

### detail propeties

bastion_ssh_config

property | context | sample value | default
-------- | ------- | ------------ | -------
name | display name | Bastion | "Bastion"
host | bastion host | localhost | Required
port | bastion port | 22 | 22
user | bastion user | ec2-user | Required
key_path | bastion ssh key path | ~/.ssh/id_rsa | "~/.ssh/id_rsa"
remote_dns | remote DNS if you use SRV Record in Tunnel settings | 10.0.0.2 | Required if use SRV

tunnels:

property | context | sample value | default
-------- | ------- | ------------ | -------
name | display name | nginx | "no name setting N"
local_bind_port | binding local port | 8080 | Required
target | target IP or Domain name | sample.your.domain | Required
target_port | target port | 80 | Required. if set target_type is "SRV" or "CNAME-SRV" then not specified.
target_type | DNS type | SRV, CNAME-SRV | Required if set target is SRV record or CNAME record that SRV is wrapped.
forwarding_timeout ** | forwarding timeout | 5s, 1m |  Optional, forwarding timeout, default 5s. must set longer time if keep forwarding over 5s(default). ex. gRPC stream.

** forwarding uses file descriptor. if set long time and many request then use many file descriptor and got too many open files error. please increase ulimit or shorter forwarding timeout.
