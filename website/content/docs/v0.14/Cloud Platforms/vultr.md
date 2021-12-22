---
title: "Vultr"
description: "Creating a cluster via the CLI (vultr-cli) on Vultr.com."
---

## Creating a cluster via the CLI
In this guide, we will create an HA Kubernetes cluster with 1 worker node.

### Create the Image
We first need to create an ISO resource targeting the current release of talos:

```bash
export VULTR_API_KEY=<key here>
vultr-cli iso create -u https://github.com/talos-systems/talos/releases/download/v0.14.0/talos-amd64.iso
```

This returns instantly, so you may need to periodically run `vultr-cli list` and monitor the iso creation until it is completed to begin the next step.

Save the image id into $IMAGE_ID, which will be needed later when starting the compute nodes.

### Create a Load Balancer
```bash
vultr-cli load-balancer create \
  -r $REGION \
  -l talos-load-balancer \
  --protocol tcp \
  --port 6443 \
  --healthy_threshold 5 \
  -c 10 \
  -t 5 \
  -u 3 \
  -f frontend_port:443,frontend_protocol:tcp,backend_port:6443,backend_protocol:tcp
```

We will also need to create a private network for our new instances:

```bash
vultr-cli network create \
-d talos-private-network \
-r $REGION \
-s 10.0.0.0 \
-z 8
```

Store the private network id in $PRIVATE_NETWORK_ID after the network creation is completed (`vultr-cli network list`)

Once our load balancer is created, we will need it's ip address in $LOAD_BALANCER_IPV4:

```bash
vultr-cli load-balancer get $LOAD_BALANCER_ID
export LOAD_BALANCER_IPV4=<IP Here>
```

### Create the Machine Configuration Files
#### Generating Base Configurations

Using the DNS name of the loadbalancer created earlier, generate the base configuration files for the Talos machines:

```bash
talosctl gen config talos-k8s-vultr-tutorial https://$LOAD_BALANCER_IPV4:443
```

At this point, you can modify the generated configs to your liking.
Optionally, you can specify `--config-patch` with RFC6902 jsonpatch which will be applied during the config generation.

#### Validate the Configuration Files

```bash
$ talosctl validate --config controlplane.yaml --mode cloud
controlplane.yaml is valid for cloud mode
$ talosctl validate --config worker.yaml --mode cloud
worker.yaml is valid for cloud mode
```

### Create the instances

#### Create the Control Plane Nodes

Run the following to give ourselves three control plane nodes:

```bash
vultr-cli instance create \
  --region $REGION \
  --iso $IMAGE_ID \
  --private-network \
  --network "$PRIVATE_NETWORK_ID" \
  --user-data "$(cat controlplane.yaml)" \
  --plan vc2-2c-4gb
```

Then, attach the new instances to the load balancer:
```bash
vultr-cli load-balancer update $LOAD_BALANCER_ID
-i "$INSTANCE_ID1,$INSTANCE_ID2,$INSTANCE_ID3"
```

To configure `talosctl`, we will need the first control plane node's IP:

```bash
vultr-cli instance get $INSTANCE_ID1 | grep 'MAIN IP'
```

Set the `endpoints` and `nodes`:

```bash
talosctl --talosconfig talosconfig config endpoint $INSTANCE_IP1
talosctl --talosconfig talosconfig config node $INSTANCE_IP1
```

Bootstrap `etcd`:

```bash
talosctl --talosconfig talosconfig bootstrap
```

### Retrieve the `kubeconfig`

At this point, we can retrive the admin kubeconfig by running:

```bash
talosctl --talosconfig talosconfig kubeconfig .
```




