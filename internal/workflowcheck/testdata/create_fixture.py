import hashlib, json, pathlib

root = pathlib.Path(__file__).parent / "corpus"
def put(path, obj):
    p = root / path; p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(obj, indent=2, sort_keys=True) + "\n")
def digest(path): return hashlib.sha256((root/path).read_bytes()).hexdigest()
def meta(kind, ident, version="0.1.0"):
    return {"apiVersion":"aok/v1alpha1","kind":kind,"metadata":{"id":ident,"name":ident,"version":version,"description":"workflowcheck fixture"}}

ident={"provider":"github","tenantId":"org","containerId":"repo","remoteId":"issue-17","itemKind":"issue"}
packet="assets/task.json"
w=meta("WorkItemBinding","issue-17");w.update(identity=ident,displayKey="ORG-17",packet={"path":packet,"sha256":digest(packet)},observedSourceRevision="rev-7",mappingVersion="github-v1",fieldMappings=[{"canonicalField":"title","providerFieldId":"title","owner":"tracker","supported":True}]);put("work-item-binding.json",w)
assignment=meta("AgentAssignment","explore-aok-001")
asspath="assignment.json"; assraw=(root/asspath).read_bytes(); ass_hash=hashlib.sha256(assraw).hexdigest()
planref={"kind":"PlanDefinition","id":"explore-plan","version":"0.1.0"}
for n,hostsession in enumerate(("sess-1","sess-2"),1):
 h=meta("HostTaskBinding",f"host-attempt-{n}");h.update(workItemRef={"kind":"WorkItemBinding","id":"issue-17","version":"0.1.0"},assignmentRef={"kind":"AgentAssignment","id":"explore-aok-001","version":"0.1.0"},host="codex",hostVersion="1",sessionId=hostsession,attemptId=f"attempt-{n}",assignmentSha256=ass_hash,planStepRef={"planRef":planref,"stepId":"inspect"});put(f"host-task-{n}.json",h)
for n in (1,2):
 r=meta("RunRecord",f"run-{n}");r.update(hostTaskBindingRef={"kind":"HostTaskBinding","id":f"host-attempt-{n}","version":"0.1.0"},runtime="local",runtimeVersion="1",runtimeRunId=f"r-{n}",attemptId=f"attempt-{n}",inputRevision="rev-7",executionProfileRef={"kind":"ExecutionProfile","id":"opencode-exploration","version":"0.1.0"},assignmentSha256=ass_hash,policyRefs=[],checkpointRefs=[],outputRefs=[],observedAt="2026-10-23T10:00:00Z",disposition="completed");put(f"run-{n}.json",r)
c=meta("ConnectorCapability","github-adapter");c.update(provider="github",adapterId="github",adapterVersion="1",scope=ident,operations=[{"operation":"update-fields","supported":True,"conditionalWrite":True,"idempotent":True,"concurrency":"conditional"}]);put("capability.json",c)
bindingref={"kind":"WorkItemBinding","id":"issue-17","version":"0.1.0"};capref={"kind":"ConnectorCapability","id":"github-adapter","version":"0.1.0"};change={"fields":[{"providerFieldId":"title","value":"Updated title"}]}
canonical={"binding":bindingref,"identity":ident,"capability":capref,"operationKind":"update-fields","expectedRevision":"rev-7","change":change}
opdigest=hashlib.sha256(json.dumps(canonical,separators=(",",":"),ensure_ascii=False).encode()).hexdigest()
o=meta("SyncOperation","update-issue-17");o.update(workItemBindingRef=bindingref,connectorCapabilityRef=capref,operationId="op-17",operationKind="update-fields",expectedRevision="rev-7",change=change,authority={"reference":"approval://ticket-1","scope":ident,"operation":"update-fields","operationDigest":opdigest},operationDigest=opdigest);put("operation.json",o)
put("assets/dispatch.json",{"status":"dispatched","operationDigest":opdigest})
readback={"operationDigest":opdigest,"identity":ident,"revision":"rev-8","applied":True,"fields":{"title":"Updated title"}};put("assets/readback.json",readback)
ev=lambda path,purpose:{"asset":{"path":path,"sha256":digest(path)},"purpose":purpose,"observedAt":"2026-10-23T10:00:00Z"}
rc=meta("SyncReceipt","receipt-17");rc.update(syncOperationRef={"kind":"SyncOperation","id":"update-issue-17","version":"0.1.0"},operationId="op-17",operationDigest=opdigest,receiptId="remote-receipt-17",observedAt="2026-10-23T10:00:00Z",disposition="applied",dispatchEvidence=[ev("assets/dispatch.json","dispatch")],readbackEvidence=[ev("assets/readback.json","readback")]);put("receipt.json",rc)
