# Tiny Systems Operator
Helm chart helps to deploy your own Tiny Systems modules

## Description
Single Helm chart for run any Tiny System module

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

## Helm charts
```shell
helm repo add tinysystems https://tiny-systems.github.io/module/
helm repo update # if you already added repo before
helm install my-corp-data-processing-tools --set controllerManager.manager.image.repository=registry.mycorp/tools/data-processing  tinysystems/tinysystems-operator
```

### Running on the cluster
1. Install Instances of Custom Resources:

```shell
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:

```shell
make docker-build docker-push IMG=<some-registry>/operator:tag
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```shell
make deploy IMG=<some-registry>/operator:tag
```


### Undeploy controller
UnDeploy the controller from the cluster:

```shell
make undeploy
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out
1. Install the CRDs into the cluster:

```shell
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```shell
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```shell
make manifests
```

### Create new api
```shell
kubebuilder create api --group operator --version v1alpha1 --kind TinySignal

```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

