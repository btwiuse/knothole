Wrangler Sample Controller
==========================

This a sample of how to use [Wrangler](https://github.com/rancher/wrangler) to write controllers.  The functionality
of the controller is intended to be the same as the upstream [sample-controller](https://github.com/kubernetes/sample-controller) project.

To run clone the repo and run.  Please use go modules.

```
make run
```

References

- https://github.com/dylanhitt/wrangler-sample/commit/2e49fe103f7f185e46125d6c0777a51ad15025dc
- https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/
- https://www.qikqiak.com/post/custom-k8s-ingress-controller-with-go/
  - https://www.doxsey.net/blog/how-to-build-a-custom-kubernetes-ingress-controller-in-go/
- https://www.doxsey.net/blog/how-to-build-a-custom-kubernetes-ingress-controller-in-go/
  - https://github.com/calebdoxsey/kubernetes-simple-ingress-controller
  - https://github.com/calebdoxsey/kubernetes-cloudflare-sync
- https://www.zeng.dev/post/2023-k8s-api-codegen/
- https://dgraph.io/blog/post/building-a-kubernetes-ingress-controller-with-caddy/
  - https://github.com/dgraph-io/ingressutil
