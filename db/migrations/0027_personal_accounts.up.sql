BEGIN;

-- Global identity data has no application table grants. Only orbitjob_admin can
-- execute the fixed SECURITY DEFINER API. Its NOLOGIN owner bypasses table RLS
-- to authenticate before a tenant is known. Tenant memberships retain explicit
-- tenant predicates and RLS. Runtime/operator roles receive no access. Rollback
-- requires restoring a backup; removing accounts must never reopen setup.
CREATE TABLE account_installation (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 claimed_at TIMESTAMPTZ
);
INSERT INTO account_installation(singleton) VALUES(TRUE);
CREATE TABLE account_users (
 id CHAR(26) PRIMARY KEY,
 username TEXT NOT NULL UNIQUE CHECK(username ~ '^[a-z0-9][a-z0-9._-]{2,63}$'),
 display_name TEXT NOT NULL DEFAULT '' CHECK(char_length(display_name)<=128),
 password_hash TEXT NOT NULL CHECK(length(password_hash) BETWEEN 64 AND 512),
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN('active','disabled')),
 installation_admin BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE account_sessions (
 id CHAR(26) PRIMARY KEY,
 user_id CHAR(26) NOT NULL REFERENCES account_users(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE CHECK(token_hash ~ '^[a-f0-9]{64}$'),
 remember BOOLEAN NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 expires_at TIMESTAMPTZ NOT NULL,
 recent_auth_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(expires_at<=created_at+INTERVAL '7 days')
);
CREATE INDEX account_session_expiry ON account_sessions(expires_at);
CREATE INDEX account_session_user ON account_sessions(user_id);
CREATE TABLE account_memberships (
 tenant_id CHAR(26) NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
 user_id CHAR(26) NOT NULL REFERENCES account_users(id) ON DELETE RESTRICT,
 role TEXT NOT NULL CHECK(role IN('admin','operator','viewer')),
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN('active','disabled')),
 boundary_policy_id CHAR(26) REFERENCES policies(id) ON DELETE RESTRICT,
 PRIMARY KEY(tenant_id,user_id)
);
CREATE INDEX account_membership_user ON account_memberships(user_id,tenant_id);
CREATE TABLE account_membership_policies (
 tenant_id CHAR(26) NOT NULL,
 user_id CHAR(26) NOT NULL,
 policy_id CHAR(26) NOT NULL REFERENCES policies(id) ON DELETE RESTRICT,
 PRIMARY KEY(tenant_id,user_id,policy_id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES account_memberships(tenant_id,user_id) ON DELETE CASCADE
);
CREATE TABLE account_capabilities (
 id CHAR(26) PRIMARY KEY,
 token_hash TEXT NOT NULL UNIQUE CHECK(token_hash ~ '^[a-f0-9]{64}$'),
 kind TEXT NOT NULL CHECK(kind IN('setup','recovery','invitation','reset')),
 username TEXT NOT NULL DEFAULT '',
 user_id CHAR(26) REFERENCES account_users(id) ON DELETE CASCADE,
 tenant_id CHAR(26) REFERENCES tenants(id) ON DELETE RESTRICT,
 grant_data JSONB NOT NULL DEFAULT '{}',
 issued_by CHAR(26) REFERENCES account_users(id) ON DELETE RESTRICT,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 consumed_at TIMESTAMPTZ
);
CREATE INDEX account_capability_expiry ON account_capabilities(expires_at);
CREATE TABLE installation_audit_events (
 id BIGSERIAL PRIMARY KEY,
 actor_id TEXT,
 event_type TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 details JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
DO $$ DECLARE obj TEXT; BEGIN
 FOREACH obj IN ARRAY ARRAY['account_installation','account_users','account_sessions','account_memberships','account_membership_policies','account_capabilities','installation_audit_events'] LOOP
  EXECUTE format('ALTER TABLE %I OWNER TO orbitjob_table_owner',obj);
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',obj);
  EXECUTE format('REVOKE ALL ON %I FROM PUBLIC,orbitjob_admin,orbitjob_runtime,orbitjob_operator',obj);
 END LOOP;
END $$;
ALTER SEQUENCE installation_audit_events_id_seq OWNER TO orbitjob_table_owner;
CREATE POLICY account_membership_tenant ON account_memberships USING(tenant_id=current_setting('app.tenant_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true));
CREATE POLICY account_member_policy_tenant ON account_membership_policies USING(tenant_id=current_setting('app.tenant_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true));
CREATE POLICY account_capability_tenant ON account_capabilities USING(tenant_id=current_setting('app.tenant_id',true)) WITH CHECK(tenant_id=current_setting('app.tenant_id',true));

INSERT INTO policies(id,tenant_id,name,description,document) VALUES(
 '00000000000000000000000013',NULL,'JobOperator','Read and operate executions inside one workspace.',
 '{"version":"1","statement":[{"effect":"Allow","action":["tenant:Get","tenant:List","group:List","job:Get","job:List","job:Trigger","instance:Get","instance:List","instance:Cancel","instance:ReadLogs","workflow:Get","workflow:List","workflow:Trigger","function:Get","function:List","function:Invoke","function:ReadLogs","check:Get","check:List","checkrun:Get","checkrun:List","sli:Get","sli:List","slo:Get","slo:List"],"resource":["orbitjob:self:*:*/*"]}]}');

-- Personal ownership is an explicit membership, never a global workload grant.
INSERT INTO policies(id,tenant_id,name,description,document) VALUES(
 '00000000000000000000000014',NULL,'ConsoleOwnerAccess','Explicit full workspace management for its human creator.',
 '{"version":"1","statement":[{"effect":"Allow","action":["tenant:Create","tenant:Get","tenant:List","apikey:Create","apikey:List","apikey:Revoke","apikey:Update","policy:Create","policy:Delete","policy:Get","policy:List","group:Create","group:List","job:Apply","job:Get","job:ManageExecution","job:RecoverDraft","job:List","job:Trigger","instance:Cancel","instance:Get","instance:ReadLogs","instance:DeleteLogs","instance:List","check:Create","check:Delete","check:Get","check:ManageExecution","check:List","check:Pause","check:Resume","checkrun:Get","checkrun:List","sli:Create","sli:Delete","sli:Get","sli:List","slo:Create","slo:Delete","slo:Get","slo:List","slo:Pause","slo:Resume","slo:GetBudget","slo:ListBudgets","sloalert:Get","sloalert:List","function:Get","function:ManageExecution","function:Create","function:Update","function:ReadLogs","function:DeleteLogs","function:Invoke","function:List","workflow:Apply","workflow:Get","workflow:ManageExecution","workflow:RecoverDraft","workflow:List","workflow:Trigger"],"resource":["orbitjob:self:*:*/*"]}]}');

-- A closed operation set avoids granting application code arbitrary cross-tenant
-- SQL. Every write and its audit record share this statement's transaction.
CREATE FUNCTION orbitjob_identity(op TEXT,p JSONB) RETURNS JSONB
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog, public AS $$
DECLARE
 u public.account_users%ROWTYPE;
 a public.account_users%ROWTYPE;
 sess public.account_sessions%ROWTYPE;
 cap public.account_capabilities%ROWTYPE;
 m public.account_memberships%ROWTYPE;
 target TEXT; tenant TEXT; role_name TEXT; grant_info JSONB; ids TEXT[]; boundary TEXT;
 result JSONB; changes JSONB; n INTEGER; deleted INTEGER; initialized BOOLEAN; sid TEXT; at_time TIMESTAMPTZ;
BEGIN
 -- Lock one installation row before any write. Concurrent demotions, disable,
 -- recovery and claims therefore cannot jointly remove the last active admin.
 IF op NOT IN('initialized','credential','session_credential','session_actor','authenticate','workspaces','membership','documents','grant_documents','sessions','members','users','workspace_directory') THEN
  PERFORM 1 FROM public.account_installation WHERE singleton FOR UPDATE;
 END IF;
 SELECT claimed_at IS NOT NULL INTO initialized FROM public.account_installation WHERE singleton;
 IF initialized IS NULL THEN RAISE EXCEPTION 'installation state missing' USING ERRCODE='XX001'; END IF;
 IF op='lock' THEN RETURN '{}'; END IF;
 IF op='initialized' THEN RETURN jsonb_build_object('initialized',initialized); END IF;
 IF op='credential' THEN
  SELECT * INTO u FROM public.account_users WHERE username=p->>'username' AND status='active';
  IF NOT FOUND THEN RAISE EXCEPTION 'authentication failed' USING ERRCODE='OJ401'; END IF;
  RETURN jsonb_build_object('user',to_jsonb(u)-'password_hash','password_hash',u.password_hash);
 END IF;
 IF op='authenticate' THEN
  SELECT * INTO sess FROM public.account_sessions WHERE token_hash=p->>'token_hash' AND expires_at>clock_timestamp();
  IF NOT FOUND THEN RAISE EXCEPTION 'authentication failed' USING ERRCODE='OJ401'; END IF;
  SELECT * INTO u FROM public.account_users WHERE id=sess.user_id AND status='active';
  IF NOT FOUND THEN RAISE EXCEPTION 'authentication failed' USING ERRCODE='OJ401'; END IF;
  RETURN (to_jsonb(sess)-'token_hash'-'user_id')||jsonb_build_object('user',to_jsonb(u)-'password_hash');
 END IF;
 IF op='login' THEN
  SELECT * INTO u FROM public.account_users WHERE id=p->>'user_id' AND status='active' AND password_hash=p->>'password_hash';
  IF NOT FOUND THEN RAISE EXCEPTION 'authentication failed' USING ERRCODE='OJ401'; END IF;
  at_time:=clock_timestamp();
  INSERT INTO public.account_sessions(id,user_id,token_hash,remember,created_at,expires_at,recent_auth_at) VALUES(p->>'id',u.id,p->>'token_hash',(p->>'remember')::boolean,at_time,at_time+(CASE WHEN (p->>'remember')::boolean THEN INTERVAL '7 days' ELSE INTERVAL '8 hours' END),at_time) RETURNING * INTO sess;
  INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(u.id,'account.login',sess.id);
  RETURN (to_jsonb(sess)-'token_hash'-'user_id')||jsonb_build_object('user',to_jsonb(u)-'password_hash');
 END IF;
 IF op='issue_installation' THEN
  IF p->>'kind' NOT IN('setup','recovery') OR (p->>'kind'='setup' AND initialized) OR (p->>'kind'='recovery' AND NOT initialized) THEN RAISE EXCEPTION 'installation state conflict' USING ERRCODE='OJ409'; END IF;
  IF (p->>'lifetime_seconds')::integer NOT BETWEEN 1 AND 3600 THEN RAISE EXCEPTION 'invalid capability lifetime' USING ERRCODE='OJ400'; END IF;
  at_time:=clock_timestamp()+make_interval(secs=>(p->>'lifetime_seconds')::integer);
  UPDATE public.account_capabilities SET consumed_at=clock_timestamp() WHERE kind IN('setup','recovery') AND consumed_at IS NULL;
  INSERT INTO public.account_capabilities(id,token_hash,kind,username,expires_at) VALUES(p->>'id',p->>'token_hash',p->>'kind',coalesce(p->>'username',''),at_time);
  INSERT INTO public.installation_audit_events(event_type,resource_id,details) VALUES('installation.capability_issued',p->>'id',jsonb_build_object('kind',p->>'kind'));
  RETURN jsonb_build_object('expires_at',at_time);
 END IF;
 IF op='setup' THEN
  SELECT * INTO cap FROM public.account_capabilities WHERE token_hash=p->>'token_hash' AND kind IN('setup','recovery') AND consumed_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
  IF NOT FOUND OR (cap.kind='setup' AND initialized) OR (cap.kind='recovery' AND NOT initialized) OR (cap.username<>'' AND cap.username<>p->>'username') THEN RAISE EXCEPTION 'invalid capability' USING ERRCODE='OJ400'; END IF;
  IF NOT EXISTS(SELECT 1 FROM public.tenants WHERE id='00000000000000000000000001' AND status='active') THEN RAISE EXCEPTION 'active default workspace required' USING ERRCODE='OJ409'; END IF;
  SELECT * INTO u FROM public.account_users WHERE username=p->>'username';
  IF FOUND AND cap.kind<>'recovery' THEN RAISE EXCEPTION 'account exists' USING ERRCODE='OJ409'; END IF;
  IF u.id IS NULL THEN
   INSERT INTO public.account_users(id,username,display_name,password_hash,installation_admin) VALUES(p->>'id',p->>'username',p->>'display_name',p->>'password_hash',TRUE) RETURNING * INTO u;
  ELSE
   UPDATE public.account_users SET password_hash=p->>'password_hash',status='active',installation_admin=TRUE WHERE id=u.id RETURNING * INTO u;
   DELETE FROM public.account_sessions WHERE user_id=u.id;
   UPDATE public.account_capabilities SET consumed_at=clock_timestamp() WHERE kind='reset' AND user_id=u.id AND consumed_at IS NULL;
  END IF;
  INSERT INTO public.account_memberships(tenant_id,user_id,role) VALUES('00000000000000000000000001',u.id,'admin') ON CONFLICT(tenant_id,user_id) DO UPDATE SET status='active',role='admin',boundary_policy_id=NULL;
  DELETE FROM public.account_membership_policies WHERE tenant_id='00000000000000000000000001' AND user_id=u.id;
  INSERT INTO public.account_membership_policies(tenant_id,user_id,policy_id) VALUES('00000000000000000000000001',u.id,'00000000000000000000000014');
  UPDATE public.account_installation SET claimed_at=coalesce(claimed_at,clock_timestamp()) WHERE singleton;
  UPDATE public.account_capabilities SET consumed_at=clock_timestamp() WHERE id=cap.id;
  INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id,details) VALUES(u.id,'installation.claimed',u.id,jsonb_build_object('kind',cap.kind));
  INSERT INTO public.audit_events(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id,diff) VALUES('00000000000000000000000001','user',u.id,'membership.created','membership',u.id,'{"role":"admin"}');
  RETURN to_jsonb(u)-'password_hash';
 END IF;
 IF op IN('workspaces','membership','documents') THEN
  SELECT * INTO u FROM public.account_users WHERE id=p->>'user_id' AND status='active';
  IF NOT FOUND THEN RAISE EXCEPTION 'authentication failed' USING ERRCODE='OJ401'; END IF;
  IF op='workspaces' THEN
   SELECT coalesce(jsonb_agg(jsonb_build_object('id',t.id,'name',t.name,'slug',t.slug,'status',t.status,'role',mm.role) ORDER BY t.name,t.id),'[]') INTO result FROM public.account_memberships mm JOIN public.tenants t ON t.id=mm.tenant_id WHERE mm.user_id=u.id AND mm.status='active' AND t.status='active';RETURN result;
  END IF;
  SELECT mm.* INTO m FROM public.account_memberships mm JOIN public.tenants t ON t.id=mm.tenant_id WHERE mm.user_id=u.id AND mm.tenant_id=p->>'tenant_id' AND mm.status='active' AND t.status='active';
  IF NOT FOUND THEN RAISE EXCEPTION 'membership required' USING ERRCODE='OJ403'; END IF;
  IF op='membership' THEN
   RETURN to_jsonb(m)||jsonb_build_object('user',to_jsonb(u)-'password_hash','policy_ids',(SELECT coalesce(jsonb_agg(mp.policy_id),'[]') FROM public.account_membership_policies mp WHERE mp.tenant_id=m.tenant_id AND mp.user_id=m.user_id));
  END IF;
  IF m.boundary_policy_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.policies WHERE id=m.boundary_policy_id AND (tenant_id IS NULL OR tenant_id=m.tenant_id)) THEN RAISE EXCEPTION 'membership boundary unavailable' USING ERRCODE='OJ403'; END IF;
  RETURN jsonb_build_object('grants',(SELECT coalesce(jsonb_agg(pol.document),'[]') FROM public.account_membership_policies mp JOIN public.policies pol ON pol.id=mp.policy_id AND (pol.tenant_id IS NULL OR pol.tenant_id=m.tenant_id) WHERE mp.tenant_id=m.tenant_id AND mp.user_id=m.user_id),'boundary',(SELECT document FROM public.policies WHERE id=m.boundary_policy_id AND (tenant_id IS NULL OR tenant_id=m.tenant_id)));
 END IF;
 IF op='grant_documents' THEN
  tenant:=p->>'tenant_id';SELECT ARRAY(SELECT jsonb_array_elements_text(p->'policy_ids')) INTO ids;boundary:=nullif(p->>'boundary_policy_id','');
  IF cardinality(ids)<1 OR cardinality(ids)>32 OR EXISTS(SELECT 1 FROM unnest(ids) i WHERE NOT EXISTS(SELECT 1 FROM public.policies WHERE id=i AND (tenant_id IS NULL OR tenant_id=tenant))) OR (boundary IS NOT NULL AND NOT EXISTS(SELECT 1 FROM public.policies WHERE id=boundary AND (tenant_id IS NULL OR tenant_id=tenant))) THEN RAISE EXCEPTION 'invalid grant policies' USING ERRCODE='OJ400'; END IF;
  RETURN jsonb_build_object('grants',(SELECT jsonb_agg(document) FROM public.policies WHERE id=ANY(ids)),'boundary',(SELECT document FROM public.policies WHERE id=boundary));
 END IF;
 IF op='reset_password' THEN
  SELECT * INTO cap FROM public.account_capabilities WHERE token_hash=p->>'token_hash' AND kind='reset' AND consumed_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'invalid capability' USING ERRCODE='OJ400'; END IF;
  UPDATE public.account_users SET password_hash=p->>'password_hash' WHERE id=cap.user_id;
  DELETE FROM public.account_sessions WHERE user_id=cap.user_id;
  UPDATE public.account_capabilities SET consumed_at=clock_timestamp() WHERE user_id=cap.user_id AND kind='reset' AND consumed_at IS NULL;
  INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(cap.user_id,'account.password_reset',cap.user_id);RETURN '{}';
 END IF;
 IF op='cleanup' THEN
  at_time:=clock_timestamp();
  n:=(p->>'limit')::integer;IF n<1 OR n>1000 THEN RAISE EXCEPTION 'invalid cleanup limit' USING ERRCODE='OJ400';END IF;
  WITH doomed AS(SELECT id FROM public.account_sessions WHERE expires_at<at_time ORDER BY expires_at LIMIT n) DELETE FROM public.account_sessions WHERE id IN(SELECT id FROM doomed);GET DIAGNOSTICS deleted=ROW_COUNT;
  WITH doomed AS(SELECT id FROM public.account_capabilities WHERE expires_at<at_time ORDER BY expires_at LIMIT n-deleted) DELETE FROM public.account_capabilities WHERE id IN(SELECT id FROM doomed);GET DIAGNOSTICS n=ROW_COUNT;
  RETURN jsonb_build_object('deleted',deleted+n);
 END IF;
 -- Invitation acceptance may create a new identity anonymously; existing
 -- identities require their own live session and never change passwords.
 IF op='accept_invitation' THEN
  SELECT * INTO cap FROM public.account_capabilities WHERE token_hash=p->>'token_hash' AND kind='invitation' AND consumed_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'invalid capability' USING ERRCODE='OJ400'; END IF;
  IF cap.username<>'' AND cap.username<>p->>'username' THEN RAISE EXCEPTION 'invitation identity mismatch' USING ERRCODE='OJ403';END IF;
  SELECT * INTO u FROM public.account_users WHERE username=p->>'username';
  IF FOUND THEN
   SELECT * INTO sess FROM public.account_sessions WHERE id=p->>'session_id' AND user_id=u.id AND expires_at>clock_timestamp();
   IF NOT FOUND OR u.status<>'active' THEN RAISE EXCEPTION 'sign in as invited account' USING ERRCODE='OJ401'; END IF;
  ELSE
   IF coalesce(p->>'session_id','')<>'' OR coalesce(p->>'password_hash','')='' THEN RAISE EXCEPTION 'new account credentials required' USING ERRCODE='OJ400'; END IF;
   INSERT INTO public.account_users(id,username,password_hash,display_name) VALUES(p->>'id',p->>'username',p->>'password_hash',p->>'display_name') RETURNING * INTO u;
  END IF;
  tenant:=cap.tenant_id;grant_info:=cap.grant_data;role_name:=grant_info->>'role';
  IF NOT EXISTS(SELECT 1 FROM public.tenants WHERE id=tenant AND status='active') THEN RAISE EXCEPTION 'workspace unavailable' USING ERRCODE='OJ409'; END IF;
  IF EXISTS(SELECT 1 FROM public.account_memberships WHERE tenant_id=tenant AND user_id=u.id) THEN RAISE EXCEPTION 'membership exists' USING ERRCODE='OJ409'; END IF;
  PERFORM public.orbitjob_identity('grant_documents',grant_info);
  INSERT INTO public.account_memberships(tenant_id,user_id,role,boundary_policy_id) VALUES(tenant,u.id,role_name,nullif(grant_info->>'boundary_policy_id',''));
  INSERT INTO public.account_membership_policies(tenant_id,user_id,policy_id) SELECT tenant,u.id,jsonb_array_elements_text(grant_info->'policy_ids');
  UPDATE public.account_capabilities SET consumed_at=clock_timestamp() WHERE id=cap.id;
  INSERT INTO public.audit_events(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id,diff) VALUES(tenant,'user',u.id,'membership.created','membership',u.id,jsonb_build_object('role',role_name,'invited_by',cap.issued_by));
  RETURN to_jsonb(u)-'password_hash';
 END IF;
 sid:=p->>'session_id';
 SELECT * INTO sess FROM public.account_sessions WHERE id=sid AND expires_at>clock_timestamp();
 IF NOT FOUND THEN RAISE EXCEPTION 'authentication required' USING ERRCODE='OJ401'; END IF;
 SELECT * INTO a FROM public.account_users WHERE id=sess.user_id AND status='active';
 IF NOT FOUND THEN RAISE EXCEPTION 'authentication required' USING ERRCODE='OJ401'; END IF;
 IF op='session_actor' THEN RETURN to_jsonb(a)-'password_hash'; END IF;
 IF op='session_credential' THEN RETURN jsonb_build_object('password_hash',a.password_hash); END IF;
 IF op='reauth' THEN
  IF a.password_hash IS DISTINCT FROM (p->>'password_hash') THEN RAISE EXCEPTION 'authentication failed' USING ERRCODE='OJ401'; END IF;
  UPDATE public.account_sessions SET recent_auth_at=clock_timestamp() WHERE id=sid;
  INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(a.id,'account.reauthenticated',sid);RETURN '{}';
 END IF;
 IF op='change_password' THEN
  IF a.password_hash IS DISTINCT FROM (p->>'password_hash') THEN RAISE EXCEPTION 'authentication failed' USING ERRCODE='OJ401'; END IF;
  UPDATE public.account_users SET password_hash=p->>'new_password_hash' WHERE id=a.id;
  DELETE FROM public.account_sessions WHERE user_id=a.id;
  UPDATE public.account_capabilities SET consumed_at=clock_timestamp() WHERE user_id=a.id AND kind='reset' AND consumed_at IS NULL;
  INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(a.id,'account.password_changed',a.id);RETURN '{}';
 END IF;
 IF op='logout' THEN DELETE FROM public.account_sessions WHERE id=sid;INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(a.id,'account.logout',sid);RETURN '{}'; END IF;
 IF op='sessions' THEN SELECT coalesce(jsonb_agg(to_jsonb(ss)-'token_hash'-'user_id' ORDER BY ss.created_at DESC),'[]') INTO result FROM public.account_sessions ss WHERE ss.user_id=a.id AND ss.expires_at>clock_timestamp();RETURN result;END IF;
 IF op<>'workspace_directory' AND sess.recent_auth_at<clock_timestamp()-INTERVAL '15 minutes' THEN RAISE EXCEPTION 'recent password required' USING ERRCODE='OJ428'; END IF;
 target:=p->>'target_id';tenant:=p->>'tenant_id';
 IF op='revoke_session' THEN
  DELETE FROM public.account_sessions WHERE id=target AND user_id=a.id;
  IF FOUND THEN
   INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(a.id,'account.session_revoked',target);
  END IF;
  RETURN '{}';
 END IF;
 IF op IN('users','update_user','create_workspace','workspace_directory') THEN
  IF NOT a.installation_admin THEN RAISE EXCEPTION 'installation administrator required' USING ERRCODE='OJ403'; END IF;
  IF op='workspace_directory' THEN SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'name',name,'slug',slug,'status',status,'role','') ORDER BY name,id),'[]') INTO result FROM public.tenants;RETURN result;END IF;
  IF op='users' THEN SELECT coalesce(jsonb_agg(to_jsonb(uu)-'password_hash' ORDER BY uu.username),'[]') INTO result FROM public.account_users uu;RETURN result;END IF;
  IF op='create_workspace' THEN
   INSERT INTO public.tenants(id,name,slug) VALUES(p->>'id',p->>'name',p->>'slug');
   INSERT INTO public.account_memberships(tenant_id,user_id,role) VALUES(p->>'id',a.id,'admin');
   INSERT INTO public.account_membership_policies(tenant_id,user_id,policy_id) VALUES(p->>'id',a.id,'00000000000000000000000014');
   INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(a.id,'workspace.created',p->>'id');
   INSERT INTO public.audit_events(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id,diff) VALUES(p->>'id','user',a.id,'membership.created','membership',a.id,'{"role":"admin"}');
   RETURN jsonb_build_object('id',p->>'id','name',p->>'name','slug',p->>'slug','status','active','role','admin');
  END IF;
  SELECT * INTO u FROM public.account_users WHERE id=target;
  IF NOT FOUND THEN RAISE EXCEPTION 'account not found' USING ERRCODE='OJ400'; END IF;
  changes:=p->'change';
  IF u.installation_admin AND u.status='active' AND (changes->>'status'='disabled' OR changes->>'installation_admin'='false') AND NOT EXISTS(SELECT 1 FROM public.account_users WHERE id<>u.id AND installation_admin AND status='active') THEN RAISE EXCEPTION 'last active administrator' USING ERRCODE='OJ412'; END IF;
  UPDATE public.account_users SET status=coalesce(changes->>'status',status),installation_admin=coalesce((changes->>'installation_admin')::boolean,installation_admin),display_name=coalesce(changes->>'display_name',display_name) WHERE id=u.id;
  IF changes->>'status'='disabled' THEN DELETE FROM public.account_sessions WHERE user_id=u.id; END IF;
  INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id,details) VALUES(a.id,'account.updated',u.id,changes-'password_hash');RETURN '{}';
 END IF;
 -- Password recovery controls the global identity and is installation-scoped.
 IF op='issue_reset' THEN
  IF NOT a.installation_admin THEN RAISE EXCEPTION 'installation administrator required' USING ERRCODE='OJ403'; END IF;
  SELECT * INTO u FROM public.account_users WHERE id=target;
  IF NOT FOUND THEN RAISE EXCEPTION 'account not found' USING ERRCODE='OJ400'; END IF;
  UPDATE public.account_capabilities SET consumed_at=clock_timestamp() WHERE kind='reset' AND user_id=target AND consumed_at IS NULL;
  INSERT INTO public.account_capabilities(id,token_hash,kind,user_id,issued_by,expires_at) VALUES(p->>'id',p->>'token_hash','reset',target,a.id,clock_timestamp()+INTERVAL '1 hour') RETURNING * INTO cap;
  INSERT INTO public.installation_audit_events(actor_id,event_type,resource_id) VALUES(a.id,'account.reset_issued',target);RETURN jsonb_build_object('expires_at',cap.expires_at);
 END IF;
 -- Workspace management needs an explicit active admin membership even when
 -- the caller is an installation administrator.
 SELECT mm.* INTO m FROM public.account_memberships mm JOIN public.tenants t ON t.id=mm.tenant_id WHERE mm.user_id=a.id AND mm.tenant_id=tenant AND mm.status='active' AND mm.role='admin' AND t.status='active';
 IF NOT FOUND THEN RAISE EXCEPTION 'workspace administrator required' USING ERRCODE='OJ403'; END IF;
 IF op='members' THEN
  SELECT coalesce(jsonb_agg(to_jsonb(mm)||jsonb_build_object('user',to_jsonb(uu)-'password_hash','policy_ids',(SELECT coalesce(jsonb_agg(mp.policy_id),'[]') FROM public.account_membership_policies mp WHERE mp.tenant_id=mm.tenant_id AND mp.user_id=mm.user_id)) ORDER BY uu.username),'[]') INTO result FROM public.account_memberships mm JOIN public.account_users uu ON uu.id=mm.user_id WHERE mm.tenant_id=tenant;RETURN result;
 END IF;
 IF op='invite' THEN
  grant_info:=p->'grant';
  IF grant_info->>'tenant_id'<>tenant THEN RAISE EXCEPTION 'workspace mismatch' USING ERRCODE='OJ400'; END IF;
  PERFORM public.orbitjob_identity('grant_documents',grant_info);
  INSERT INTO public.account_capabilities(id,token_hash,kind,username,tenant_id,grant_data,issued_by,expires_at) VALUES(p->>'id',p->>'token_hash','invitation',coalesce(grant_info->>'username',''),tenant,grant_info,a.id,clock_timestamp()+INTERVAL '24 hours') RETURNING * INTO cap;
  INSERT INTO public.audit_events(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id,diff) VALUES(tenant,'user',a.id,'invitation.created','invitation',p->>'id',jsonb_build_object('role',grant_info->>'role'));RETURN jsonb_build_object('expires_at',cap.expires_at);
 END IF;
 IF op='update_member' THEN
  changes:=p->'change';SELECT * INTO m FROM public.account_memberships WHERE user_id=target AND tenant_id=tenant;
  IF NOT FOUND THEN RAISE EXCEPTION 'member not found' USING ERRCODE='OJ400';END IF;
  role_name:=coalesce(changes->>'role',m.role);
  IF changes ? 'policy_ids' AND jsonb_array_length(changes->'policy_ids')>0 THEN SELECT ARRAY(SELECT jsonb_array_elements_text(changes->'policy_ids')) INTO ids;
  ELSIF changes ? 'role' THEN ids:=ARRAY[CASE role_name WHEN 'admin' THEN '00000000000000000000000011' WHEN 'operator' THEN '00000000000000000000000013' WHEN 'viewer' THEN '00000000000000000000000012' END];
  ELSE SELECT array_agg(policy_id::text) INTO ids FROM public.account_membership_policies WHERE user_id=target AND tenant_id=tenant;END IF;
  boundary:=CASE WHEN changes ? 'boundary_policy_id' THEN nullif(changes->>'boundary_policy_id','') ELSE m.boundary_policy_id END;
  PERFORM public.orbitjob_identity('grant_documents',jsonb_build_object('tenant_id',tenant,'policy_ids',to_jsonb(ids),'boundary_policy_id',boundary));
  UPDATE public.account_memberships SET role=role_name,status=coalesce(changes->>'status',status),boundary_policy_id=boundary WHERE tenant_id=tenant AND user_id=target;
  DELETE FROM public.account_membership_policies WHERE tenant_id=tenant AND user_id=target;
  INSERT INTO public.account_membership_policies(tenant_id,user_id,policy_id) SELECT tenant,target,unnest(ids);
  INSERT INTO public.audit_events(tenant_id,actor_type,actor_id,event_type,resource_type,resource_id,diff) VALUES(tenant,'user',a.id,'membership.updated','membership',target,changes);RETURN '{}';
 END IF;
 RAISE EXCEPTION 'unsupported identity operation' USING ERRCODE='OJ400';
END $$;
ALTER FUNCTION orbitjob_identity(TEXT,JSONB) OWNER TO orbitjob_table_owner;
REVOKE ALL ON FUNCTION orbitjob_identity(TEXT,JSONB) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION orbitjob_identity(TEXT,JSONB) TO orbitjob_admin;
COMMIT;
