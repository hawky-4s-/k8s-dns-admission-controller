# Task 4.1: Base Helm Chart

**Phase**: 4 - Helm Chart  
**Estimate**: 3-4 hours  
**Dependencies**: Phase 2, Phase 3

## Objective

Create the base Helm chart structure with Deployment, Service, RBAC, and configurable values.

## Deliverables

- [ ] Chart structure in `charts/k8s-dns-admission-controller/`
- [ ] `Chart.yaml` with metadata
- [ ] `values.yaml` with all configuration
- [ ] Deployment template
- [ ] Service template
- [ ] ServiceAccount and RBAC

## Chart Structure

```
charts/k8s-dns-admission-controller/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── _helpers.tpl
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── serviceaccount.yaml
│   ├── rbac.yaml
│   └── NOTES.txt
```

## Key values.yaml Settings

- `replicaCount`
- `image.repository`, `image.tag`
- `ndots.value`, `ndots.annotationKey`, `ndots.annotationMode`
- `namespaceExclude`, `namespaceInclude`
- `resources`, `nodeSelector`, `tolerations`

## Acceptance Criteria

- [ ] `helm lint` passes
- [ ] `helm template` renders valid manifests
- [ ] All configuration exposed via values
- [ ] NOTES.txt provides usage guidance
