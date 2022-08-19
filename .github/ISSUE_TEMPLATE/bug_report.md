---
name: Bug report
about: Create a report to help us improve Capsule-Proxy
title: ''
labels: blocked-needs-validation, bug
assignees: ''

---

<!--
Thanks for taking time reporting a Capsule-Proxy bug!
-->

# Bug description

A clear and concise description of what the bug is.

# How to reproduce

Steps to reproduce the behavior:

# Expected behavior

A clear and concise description of what you expected to happen.

# Logs

If applicable, please provide logs of `capsule`.

In a standard stand-alone installation of Capsule,
you'd get this by running `kubectl -n capsule-system logs deploy/capsule-proxy`.

# Additional context

- Capsule-Proxy version: (`capsule-proxy --version`)
- Helm Chart version: (`helm list -n capsule-system`)
- Kubernetes version: (`kubectl version`)