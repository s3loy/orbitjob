-- Roll the application back before removing the bounded platform reader.
DROP FUNCTION IF EXISTS public.orbitjob_platform_key_documents(text);
