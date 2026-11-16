import { Button, Checkbox, Flex, Heading, Text, TextField } from '@radix-ui/themes';

export type ExecutionResources = {
  requests?: Record<string, string>;
  limits?: Record<string, string>;
};
type Settings = { version: number; retrySafe: boolean; resources?: ExecutionResources };

export function ExecutionResourcesFields({
  version,
  retrySafe,
  resources,
  disabled = false,
  change,
}: Settings & { disabled?: boolean; change: (settings: Settings) => void }) {
  const dimensions = [
    ['cpu', 'CPU'],
    ['memory', 'Memory'],
    ['ephemeral-storage', 'Temporary storage'],
  ] as const;
  function edit(group: 'requests' | 'limits', name: string, value: string) {
    const values = { ...resources?.[group] };
    if (value.trim()) values[name] = value;
    else delete values[name];
    change({ version, retrySafe, resources: { ...resources, [group]: values } });
  }
  return (
    <section className="authoring-settings-group">
      <Heading as="h2" size="3" weight="medium">
        Resources
      </Heading>
      <div>
        {version === 0 && (
          <Button
            size="1"
            variant="outline"
            disabled={disabled}
            onClick={() => change({ version: 1, retrySafe: false, resources })}
          >
            Set resource limits
          </Button>
        )}
        {version === 1 && (
          <>
            <div className="manifest-fields">
              {dimensions.flatMap(([name, label]) =>
                (['requests', 'limits'] as const).map((group) => (
                  <label className="form-field" key={`${group}-${name}`}>
                    {label} {group === 'requests' ? 'request' : 'limit'}
                    <TextField.Root
                      aria-label={`${label} ${group === 'requests' ? 'request' : 'limit'}`}
                      disabled={disabled}
                      value={resources?.[group]?.[name] ?? ''}
                      placeholder="Use default"
                      onChange={(event) => edit(group, name, event.target.value)}
                    />
                  </label>
                )),
              )}
            </div>
            <Flex mt="3" align="center" gap="2" asChild>
              <Text as="label" size="2">
                <Checkbox
                  disabled={disabled}
                  checked={retrySafe}
                  onCheckedChange={(value) =>
                    change({ version, retrySafe: value === true, resources })
                  }
                />{' '}
                This task is safe to repeat
              </Text>
            </Flex>
          </>
        )}
      </div>
    </section>
  );
}
