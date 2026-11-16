import { describe, expectTypeOf, it } from 'vitest';
import type { components } from './schema';

type Schemas = components['schemas'];
type RequiredKeys<T> = {
  [K in keyof T]-?: object extends Pick<T, K> ? never : K;
}[keyof T];

// These compile-time assertions run with the normal typecheck gate. Regenerating
// with defaultNonNullable enabled must fail even if runtime fixtures supply defaults.
describe('generated input and response contracts', () => {
  it('allows omitted check defaults while retaining the required target', () => {
    expectTypeOf<{
      name: 'Probe';
      check_type: 'http_health';
      check_config: { url: string };
      schedule_type: 'interval';
      interval_sec: number;
    }>().toExtend<Schemas['CreateCheckRequest']>();
    expectTypeOf<RequiredKeys<Schemas['CreateCheckRequest']>>().toEqualTypeOf<
      'name' | 'check_type' | 'check_config'
    >();
    expectTypeOf<{
      url: string;
      method: null;
      expected_status: null;
    }>().toExtend<Schemas['CreateCheckRequest']['check_config']>();
    expectTypeOf<{ method: 'GET' }>().not.toExtend<Schemas['CreateCheckRequest']['check_config']>();
    expectTypeOf<Omit<Schemas['CreateCheckRequest'], 'name'>>().not.toExtend<
      Schemas['CreateCheckRequest']
    >();
  });

  it('allows omitted SLI defaults while requiring an exact source identity', () => {
    expectTypeOf<{
      name: 'Availability';
      sli_type: 'availability';
      source_config: { source_uid: string };
      good_event_criteria: { status: 'success' };
    }>().toExtend<Schemas['CreateSLIRequest']>();
    expectTypeOf<RequiredKeys<Schemas['CreateSLIRequest']>>().toEqualTypeOf<
      'name' | 'sli_type' | 'source_config'
    >();
    expectTypeOf<Record<string, unknown>>().not.toExtend<
      Schemas['CreateSLIRequest']['source_config']
    >();
  });

  it('allows omitted SLO thresholds while retaining its required objective fields', () => {
    expectTypeOf<{
      name: 'Availability';
      sli_id: string;
      target: number;
      window_duration: '24h';
    }>().toExtend<Schemas['CreateSLORequest']>();
    expectTypeOf<RequiredKeys<Schemas['CreateSLORequest']>>().toEqualTypeOf<
      'name' | 'sli_id' | 'target' | 'window_duration'
    >();
    expectTypeOf<Omit<Schemas['CreateSLORequest'], 'sli_id'>>().not.toExtend<
      Schemas['CreateSLORequest']
    >();
  });

  it('keeps defaulted function fields optional and update version required', () => {
    expectTypeOf<{ name: 'Task'; image: 'busybox:1.37' }>().toExtend<
      Schemas['FunctionDefinitionRequest']
    >();
    expectTypeOf<RequiredKeys<Schemas['FunctionDefinitionRequest']>>().toEqualTypeOf<
      'name' | 'image'
    >();
    expectTypeOf<RequiredKeys<Schemas['UpdateFunctionRequest']>>().toEqualTypeOf<
      'name' | 'image' | 'expected_version'
    >();
    expectTypeOf<Omit<Schemas['UpdateFunctionRequest'], 'expected_version'>>().not.toExtend<
      Schemas['UpdateFunctionRequest']
    >();
  });

  it('separates normalized assertion inputs from always serialized stored responses', () => {
    expectTypeOf<RequiredKeys<Schemas['AssertionRule']>>().toEqualTypeOf<
      'metric' | 'operator' | 'severity'
    >();
    expectTypeOf<RequiredKeys<Schemas['CheckAssertionRule']>>().toEqualTypeOf<
      'metric' | 'operator' | 'severity' | 'threshold'
    >();
    expectTypeOf<Schemas['AssertionRule']['operator']>().toEqualTypeOf<
      '>' | '<' | '==' | '!=' | '>=' | '<='
    >();
    expectTypeOf<Schemas['CheckAssertionRule']['operator']>().toEqualTypeOf<string>();
  });
});
