# Container Commands

Available commands:

- [create](#create)
- [list](#list)
- [terminate](#terminate)
- [client](#client)
- [dispatch](#dispatch)


## create

Creates a new container with the given root flist, mount points and ZeroTier network ID, and connects it to the given bridges.

Arguments:

```javascript
{
  'root': {root_url},
  'mount': {mount},
  'host_network': {host_network},
  'nics': [{
      'type': {nic_type},
      'id': {id},
      'name': {name},
      'hwaddr': {hwaddr},
      'config': {
          'dhcp': {dfhcp},
          'cidr': {cidr},
          'gateway': {gateway},
          'dns': {dns}
        }
  }],
  'port': {port},
  'hostname': {hostname},
  'privileged': {privileged},
  'storage': {storage},
  'tags': {tags}
}
```

Values:

- **{root_url}**: URL of the flist for the root filesystem, e.g. `https://hub.gig.tech/gig-official-apps/ubuntu1604.flist`

- **{mount}**: Dict of `('{host_source}': '{container_target}')` pairs, each mounting a directory on the host or a flist (specified by its URL) to the container

- **{host_network}**: True or false, specifying whether the container should share the same network stack as the host
  - If True, all below ZeroTier, bridge and port arguments are ignored

- **nics**: Dict of "nic" objects, defined by following values:

  - **{nic_type}**: Type of network, possible values are:
    - `default`
    - `bridge`
    - `zerotier`
    - `vlan` (only supported by Open vSwitch)
    - `vxlan` (only supported by Open vSwitch)

  - **{id}**: (optional) Depending on the value for {nice_type}:
    - Name of the bridge
    - ZeroTier network id
    - VLAM tag
    - VXLAM network identifier (VNID)

  - **{name}**: Name of the NIC inside the container

  - **{hwaddr}**: (optional) MAC address

  - **{config}**: Only relevant for bridge, VLAN and VXLAN types:  
    - `{dhcp}`: True/False. Runs the `Udhcpc` DHCP client on the container link, of course this will only work if the bridge is created with `dnsmasq` networking
    - `{CIDR}`: Assigns a static IP address to the link
    - `{gateway}`: gateway
    - `{dns}`: dns

- **port**: Dict of `{host_port}: {container_port}` pairs

  Example: `port=[8080: 80, 7000:7000]`

- **{hostname}**: Specific hostname you want to give to the container
  - If none it will automatically be set to `core-x`, x being the ID of the container

- **{privileged}**: True/False. When True the container has privileged access to the host devices, the default is False, isolating the container from the host.

- **{storage}**: URL to the ARDB storage cluster to mount, e.g. `ardb://hub.gig.tech:16379`
  - If not provided the default one from the Zero-OS main configuration will be used, see the documentation about `storage` in [Main Configuration](../../config/main.md) for more details
- **{tags}**: List of labels (strings) that you can attach to a container, can be used to to search all containers matching a specified set of tags; see the `find()` command


## list

Lists all available containers on a host. It takes no arguments.


## find

Finds containers that matches set of tags.

Arguments:
```javascript
{
    "tags": {tags},
}
```

## terminate

Destroys the container and stops the core processes. It takes a mandatory container ID.

Arguments:
```javascript
{
    "container": container_id,
}
```


### client

Returns a container instance.


## dispatch

Dispatches any given command to the 0-core of the container.

Arguments:
```javascript
{
     "container": core_id,
     "command": {
         //the full command payload
     }
}
```
