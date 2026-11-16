export type ValidationIssue = {
  instancePath: string;
  message?: string;
  params: { missingProperty?: string };
};
export type Validator = { (value: unknown): boolean; errors?: ValidationIssue[] | null };
export const validateScheduledJob: Validator;
export const validateWorkflow: Validator;
