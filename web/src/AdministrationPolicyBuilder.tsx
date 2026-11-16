import { useState } from 'react';
import { Button, Checkbox, Flex, Text, TextArea, TextField } from '@radix-ui/themes';
import { Code2, ListChecks } from 'lucide-react';
import {
  guidedActions,
  guidedPolicy,
  formatPolicyDocument,
  policyDocument,
  policyTemplates,
} from './administration-policy';
import { NamedPicker, type NamedOption } from './AdministrationFields';

export function AdministrationPolicyBuilder({
  value,
  onChange,
  groups,
  advanced,
  onAdvancedChange,
}: {
  value: string;
  onChange(value: string): void;
  groups: NamedOption[];
  advanced: boolean;
  onAdvancedChange(value: boolean): void;
}) {
  const [message, setMessage] = useState('');
  const guided = guidedPolicy(value);
  function format() {
    try {
      const formatted = formatPolicyDocument(value);
      onChange(formatted);
      setMessage('JSON formatted. Grant permissions are checked when saved.');
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Enter valid policy JSON.');
    }
  }
  return (
    <section className="admin-field-group" aria-label="Policy permissions">
      <Flex gap="2" wrap="wrap">
        <Button
          type="button"
          aria-pressed={!advanced}
          variant={!advanced ? 'solid' : 'outline'}
          color="gray"
          disabled={!guided}
          onClick={() => onAdvancedChange(false)}
        >
          <ListChecks size={14} /> Guided builder
        </Button>
        <Button
          type="button"
          aria-pressed={advanced}
          variant={advanced ? 'solid' : 'outline'}
          color="gray"
          onClick={() => onAdvancedChange(true)}
        >
          <Code2 size={14} /> Advanced JSON
        </Button>
      </Flex>
      {!advanced && guided ? (
        <>
          <label>
            Task template
            <select
              aria-label="Policy task template"
              value={
                Object.entries(policyTemplates).find(
                  ([, template]) =>
                    template.actions.length === guided.actions.length &&
                    template.actions.every((action) => guided.actions.includes(action)),
                )?.[0] ?? 'custom'
              }
              onChange={(event) => {
                const template =
                  policyTemplates[event.target.value as keyof typeof policyTemplates];
                if (template) onChange(policyDocument(template.actions, guided.scope));
              }}
            >
              {Object.entries(policyTemplates).map(([id, template]) => (
                <option key={id} value={id}>
                  {template.name}
                </option>
              ))}
              <option value="custom" disabled>
                Custom selection
              </option>
            </select>
          </label>
          <Text as="p" size="2" color="gray">
            {Object.values(policyTemplates).find(
              (template) =>
                template.actions.length === guided.actions.length &&
                template.actions.every((action) => guided.actions.includes(action)),
            )?.description ?? 'Only the checked actions will be included.'}
          </Text>
          <details className="admin-advanced">
            <summary>Review and adjust {guided.actions.length} explicit actions</summary>
            <div className="admin-action-options">
              {guidedActions.map((action) => (
                <label key={action}>
                  <Checkbox
                    disabled={guided.actions.length === 1 && guided.actions.includes(action)}
                    checked={guided.actions.includes(action)}
                    onCheckedChange={(checked) =>
                      onChange(
                        policyDocument(
                          checked
                            ? [...guided.actions, action]
                            : guided.actions.filter((item) => item !== action),
                          guided.scope,
                        ),
                      )
                    }
                  />
                  {action}
                </label>
              ))}
            </div>
          </details>
          <div className="admin-scope">
            <Text as="p" weight="medium">
              Scope
            </Text>
            <NamedPicker
              label="Resource scope"
              value={guided.scope.group}
              options={[{ id: '*', name: 'All resource groups in this workspace' }, ...groups]}
              onChange={(group) =>
                onChange(policyDocument(guided.actions, { ...guided.scope, group }))
              }
            />
            <Text as="p" size="1" color="gray">
              Jobs and Runs require an ungrouped key.
            </Text>
            <details className="admin-advanced">
              <summary>Limit resource type or exact resource ID</summary>
              <label>
                Resource type
                <select
                  aria-label="Policy resource type"
                  value={guided.scope.type}
                  onChange={(event) =>
                    onChange(
                      policyDocument(guided.actions, { ...guided.scope, type: event.target.value }),
                    )
                  }
                >
                  <option value="*">All selected task types</option>
                  {[...new Set(guidedActions.map((action) => action.split(':')[0]))].map((type) => (
                    <option key={type} value={type}>
                      {type}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Resource ID
                <TextField.Root
                  aria-label="Policy resource ID"
                  required
                  value={guided.scope.id}
                  onChange={(event) =>
                    onChange(
                      policyDocument(guided.actions, { ...guided.scope, id: event.target.value }),
                    )
                  }
                />
              </label>
              <Text as="p" size="1">
                Use * for every resource of the selected types. An exact ID narrows the grant; list
                actions may need a separate list rule in Advanced JSON.
              </Text>
            </details>
          </div>
          <details className="admin-advanced">
            <summary>Preview generated policy JSON</summary>
            <pre>{value}</pre>
          </details>
        </>
      ) : (
        <>
          <label>
            Policy document
            <TextArea
              aria-label="Policy document"
              rows={12}
              required
              value={value}
              onChange={(event) => {
                onChange(event.target.value);
                setMessage('');
              }}
            />
          </label>
          <Text as="p" size="2" color="gray">
            Use explicit supported actions and orbitjob:self:GROUP:TYPE/ID resources. Allow and Deny
            statements are submitted exactly as written.
          </Text>
          {!guided && (
            <Text as="p" size="1">
              This document has rules the guided builder cannot represent. Keep editing JSON to
              preserve them.
            </Text>
          )}
          <Button type="button" variant="outline" onClick={format}>
            Validate and format JSON
          </Button>
          {message && (
            <Text as="p" size="2" role="status">
              {message}
            </Text>
          )}
        </>
      )}
    </section>
  );
}
