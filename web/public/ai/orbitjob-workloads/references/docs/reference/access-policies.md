> Repository source: docs/reference/access-policies.md. References marked repository require a source checkout.

# Access policy reference

A policy combines action grants with resource patterns. It does not create a
workspace membership, provision a Kubernetes namespace or establish a database
connection. Read [manage access](../tasks/manage-access.md) for the complete
human-member and automation-key procedures.

## Documents and statements

A policy document accepts a `version` string and requires a nonempty `statement` array. Each statement
has `effect` (`Allow` or `Deny`), a nonempty `action` array and a nonempty
`resource` array. Field names are exact lowercase names; unknown fields,
duplicate keys and unsupported document structures are rejected. There is no
arbitrary condition-language field.

The following read-only pattern illustrates the document shape. Replace the
tenant with the actual target before creating a policy:

```json
{
  "version": "1",
  "statement": [
    {
      "effect": "Allow",
      "action": ["job:List", "job:Get", "instance:List", "instance:Get"],
      "resource": [
        "orbitjob:YOUR_TENANT:*:job/*",
        "orbitjob:YOUR_TENANT:*:instance/*"
      ]
    }
  ]
}
```

This example grants neither execution nor log access. Use the declared action
for every operation; `job:Trigger`, `instance:Cancel`, `instance:ReadLogs` and
`instance:DeleteLogs` are separate capabilities. Tenant-authored policies use
supported exact actions. Platform presets can carry wildcard actions; a tenant
cannot obtain their authority by copying such a document.

## Resource patterns

The resource shape is `orbitjob:<tenant>:<group>:<type>/<id>`. Each segment must
be present. `*` matches an entire segment; it is not a substring pattern.
A legacy empty group segment also matches all groups. Use explicit group
patterns for new policies when supported by the operation.

Target scope is resolved by the server from authenticated identity and persisted
resource ownership. A caller cannot supply an arbitrary target ARN to bypass a
route's resolver. Group-bound credentials are rejected by routes that do not
support group restrictions, including ordinary Job/Instance/Workflow and typed
definition operations. Functions have their own persisted group scope.

## Evaluation and delegation

No applicable Allow means denial. An applicable Deny overrides Allow. Permission
boundaries restrict effective grants; they do not grant actions by themselves.
Policy, membership and key delegation must remain within the caller's effective
permissions and boundary. An ambiguous or unprovable intersection is rejected.

A platform credential must select the authorized target tenant on tenant-data
routes. A tenant credential remains bound to its own tenant. Workspace discovery
and installation-administrator status grant no implicit workload capability.
Check each operation's action and actual parent resource; use
`/api/v1/identity` for authenticated identity and supported permission inspection.
A positive permission decision is not proof that the resource exists.

Read the security boundaries (repository: `../../SECURITY.md`) before delegating policy or
API-key administration. Recheck a denied action against explicit Deny, resource
group, boundary and the selected workspace; changing role labels cannot bypass
them.
