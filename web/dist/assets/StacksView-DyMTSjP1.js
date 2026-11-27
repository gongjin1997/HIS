import{E as rt}from"./index-CTQNYoKC.js";import{_ as A,a as J,u as lt,k as ct,b as dt,c as M}from"./index-6ihMuYft.js";import{E as ut}from"./index-ChqNXlTi.js";import{E as pt,a as mt}from"./index-lSXJ2rhI.js";import{v as kt}from"./directive-CrkeiOCW.js";import{E as x}from"./index-CrfIuXms.js";import{d as L,a as b,g as i,u as r,b as e,I as y,F as D,M as R,e as P,j as S,c as j,o as l,f as h,w as ft,m as gt,T,a8 as vt,a1 as C,J as _t,k as yt,ag as q,ao as ht}from"./vue-vendor-DHKnONa8.js";import{K as Q}from"./KiStatusBadge-DKW9jdwC.js";import{K as bt}from"./KiEnvBadge-KRs7Cycq.js";import{K as X}from"./KiBanner-P0m5nRbQ.js";import{K as wt}from"./KiDangerConfirmModal-DWHGHyu8.js";import{H as G,P as F,V as St,B as $t,C as Ct,M as Vt}from"./xyflow-Z5RJe8OO.js";import{K as xt}from"./KiDomainTag-OmyeMoOk.js";import{B as Pt,m as Ot,o as Tt,z as jt,D as It,u as Et}from"./icons-CeLPqGMx.js";/* empty css                                                                       */import"./typescript-BzgFJbzT.js";import"./index-BRMtJ9yX.js";import"./xterm-pi3tdajZ.js";import"./omit-PKFchG9i.js";import"./_baseIteratee-B1KnqVEc.js";import"./index-DZ3qkj-G.js";import"./KiButton-CN0L-xE7.js";function Mt(_){const u=new Map;for(const p of _)u.set(p.id,p.dependsOn||[]);const o=new Map,v=[],w=p=>{o.set(p,1),v.push(p);for(const k of u.get(p)||[]){if(!u.has(k))continue;const n=o.get(k)||0;if(n===1){const c=v.indexOf(k);return[...v.slice(c),k]}if(n===0){const c=w(k);if(c)return c}}return v.pop(),o.set(p,2),null};for(const p of _)if((o.get(p.id)||0)===0){const k=w(p.id);if(k)return{hasCycle:!0,cyclePath:k}}return{hasCycle:!1,cyclePath:[]}}const zt={class:"ki-dag-node ki-surface"},Kt={class:"ki-dag-node__head"},Bt={class:"ki-flex-row ki-truncate"},Dt={class:"ki-mono ki-truncate"},Rt={class:"ki-dag-node__body"},At={class:"ki-flex-between ki-dag-meta"},Lt={class:"ki-dag-node__prod ki-mono"},Nt={key:0,class:"ki-dag-ports"},Yt={class:"ki-mono"},Ht={key:0,class:"ki-dag-port ki-dag-port--more ki-mono"},Ut={class:"ki-dag-node__foot ki-muted"},qt=L({__name:"KiStackDagNode",props:{data:{}},setup(_){const u=_,o=j(()=>(u.data.outputs||[]).slice(0,4)),v=j(()=>Math.max(0,(u.data.outputs||[]).length-4));return(w,p)=>{var k;return l(),b("div",zt,[i(r(G),{type:"target",position:r(F).Left,class:"ki-dag-handle"},null,8,["position"]),e("header",Kt,[e("div",Bt,[i(r(Pt),{size:14,class:"ki-dag-node__icon","aria-hidden":"true"}),e("strong",Dt,y(_.data.id),1)]),i(Q,{status:_.data.status||"ready"},null,8,["status"])]),e("div",Rt,[e("div",At,[e("span",Lt,y(_.data.product)+" "+y(_.data.version),1),i(xt,{kind:"target",value:_.data.target},null,8,["value"])]),o.value.length?(l(),b("div",Nt,[(l(!0),b(D,null,R(o.value,n=>(l(),b("div",{key:n.name,class:"ki-dag-port"},[n.secret?(l(),P(r(Ot),{key:0,size:10,class:"ki-text-warn","aria-hidden":"true"})):S("",!0),e("span",Yt,y(n.name),1)]))),128)),v.value>0?(l(),b("span",Ht,"+"+y(v.value),1)):S("",!0)])):S("",!0)]),e("footer",Ut,[e("span",null,"上游依赖 "+y(_.data.dependsOnCount??0),1),e("span",null,"输出项 "+y(((k=_.data.outputs)==null?void 0:k.length)??0),1)]),i(r(G),{type:"source",position:r(F).Right,class:"ki-dag-handle"},null,8,["position"])])}}}),Gt=A(qt,[["__scopeId","data-v-a031315c"]]),Ft={class:"ki-dag-wrap"},Wt={class:"ki-dag-canvas ki-surface"},Jt=344,W=242,Qt=48,Xt=36,Zt=L({__name:"StackCanvas",props:{instances:{}},setup(_){const u=_,o=J(),v=j(()=>Mt(u.instances.map(n=>({id:n.id,dependsOn:n.dependsOn||[]})))),w=j(()=>{const n=new Map,c=(a,g=new Set)=>{if(n.has(a))return n.get(a);if(g.has(a))return 0;g.add(a);const d=u.instances.find(O=>O.id===a);if(!d||!d.dependsOn.length)return n.set(a,0),0;const $=1+Math.max(...d.dependsOn.map(O=>c(O,g)));return n.set(a,$),$};u.instances.forEach(a=>c(a.id));const f=new Map;for(const a of u.instances){const g=n.get(a.id)||0;f.set(g,(f.get(g)||0)+1)}const I=Math.max(1,...Array.from(f.values())),E=new Map;return u.instances.map(a=>{const g=n.get(a.id)||0,d=E.get(g)||0;E.set(g,d+1);const $=f.get(g)||1,O=(I-$)*W/2;return{id:a.id,type:"kiStackNode",position:{x:Qt+g*Jt,y:Xt+O+d*W},data:{id:a.id,product:a.product,version:a.version,target:a.target,status:a.status||"ready",dependsOnCount:(a.dependsOn||[]).length,outputs:a.outputs||[{name:"endpoint"},{name:"rootPassword",secret:!0}]}}})}),p=j(()=>{const n=[];for(const c of u.instances)for(const f of c.dependsOn||[])n.push({id:`${f}->${c.id}`,source:f,target:c.id,type:"smoothstep",animated:c.status==="running",markerEnd:Vt.ArrowClosed});return n}),k=j(()=>u.instances.map(n=>`${n.id}:${(n.dependsOn||[]).join(",")}`).join("|"));return(n,c)=>(l(),b("div",Ft,[v.value.hasCycle?(l(),P(X,{key:0,variant:"danger",text:`检测到应用栈循环依赖：${v.value.cyclePath.join(" → ")}，已自动阻断部署计划`,class:"ki-mb-3"},null,8,["text"])):S("",!0),e("div",Wt,[(l(),P(r(St),{key:k.value,nodes:w.value,edges:p.value,"color-mode":r(o).theme,"min-zoom":.45,"max-zoom":1.5,"fit-view-options":{padding:.2,maxZoom:1},"fit-view-on-init":""},{"node-kiStackNode":h(f=>[i(Gt,{data:f.data},null,8,["data"])]),default:h(()=>[i(r($t),{variant:"dots",gap:20,size:1.2}),i(r(Ct),{position:"bottom-right","show-interactive":!1})]),_:1},8,["nodes","edges","color-mode"]))])]))}}),te=A(Zt,[["__scopeId","data-v-3f9a92ae"]]),ee={class:"ki-card ki-mb-4"},ae={class:"ki-card__title"},se={class:"ki-flex-row"},ne={key:1,class:"dag-layers ki-mb-3"},oe={key:0,class:"ki-badge ki-mono ext-pill"},ie={class:"ki-grid-2"},re={class:"ki-card"},le={class:"ki-card__title"},ce={class:"ki-pill-tabs ki-mb-3"},de=["onClick"],ue={class:"ki-card"},pe={class:"ki-card__title"},me={class:"sub"},ke={class:"ki-mono"},fe={class:"ki-row-actions"},ge=L({__name:"StacksView",setup(_){const u=lt(),o=J(),v=ht(),w={logging:{label:"日志分析栈 (S3 + Kafka + OpenSearch + DataPrepper)",name:"prod-t3-logging-platform",yaml:`apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: prod-t3-logging-platform
spec:
  instances:
    - id: s3
      product: object-storage
      productVersion: "4.54.0"
      deployTarget: host-binary
    - id: kafka
      product: kafka
      productVersion: "4.3.2"
      deployTarget: host-binary
    - id: opensearch
      product: opensearch
      productVersion: "3.10.0"
      deployTarget: host-binary
      dependsOn: [s3]
    - id: dataprepper
      product: elk-pipeline
      productVersion: "2.16.0"
      deployTarget: host-binary
      dependsOn: [kafka, opensearch, s3]
`},hisCore:{label:"核心交易栈 (PostgreSQL + Valkey + Kafka)",name:"his-core-app-stack",yaml:`apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: his-core-app-stack
spec:
  instances:
    - id: pg
      product: postgresql
      productVersion: "18.3"
      deployTarget: host-binary
    - id: cache
      product: valkey
      productVersion: "9.1.4"
      deployTarget: host-binary
    - id: mq
      product: kafka
      productVersion: "4.3.2"
      deployTarget: host-binary
      dependsOn: [pg, cache]
`},devops:{label:"研发交付栈 (PG + Valkey + GitLab + Harbor)",name:"devops-platform-stack",yaml:`apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: devops-platform-stack
spec:
  instances:
    - id: db
      product: postgresql
      productVersion: "18.3"
      deployTarget: host-binary
    - id: cache
      product: valkey
      productVersion: "9.1.4"
      deployTarget: host-binary
    - id: gitlab
      product: gitlab
      productVersion: "18.10.0"
      deployTarget: host-container
      dependsOn: [db, cache]
    - id: harbor
      product: harbor
      productVersion: "2.15.0"
      deployTarget: host-container
      dependsOn: [db, cache]
`}},p=C(!1),k=C(!1),n=C([]),c=C(w.logging.name),f=C(w.logging.yaml),I=C("logging"),E=C([]),a=C(null),g=C(""),d=_t({open:!1,stack:null,loading:!1});async function $(){if(!(!o.currentProjectId||!f.value.trim())){k.value=!0,g.value="";try{const{data:s}=await M("POST",`/api/v1/projects/${o.currentProjectId}/stacks:validate`,{body:{specYaml:f.value}});a.value=s.dag,E.value=(s.nodes||[]).map(t=>({...t,status:"ready"}))}catch(s){g.value=String(s),a.value=null}finally{k.value=!1}}}function O(s){const t=w[s];t&&(I.value=s,c.value=t.name,f.value=t.yaml,$())}function Z(s){I.value="",c.value=String(s.name||""),typeof s.specYaml=="string"&&s.specYaml.trim()&&(f.value=s.specYaml,$())}async function z(){if(o.currentProjectId){p.value=!0;try{const{data:s}=await M("GET",`/api/v1/projects/${o.currentProjectId}/stacks`);n.value=s.items||[],await $()}finally{p.value=!1}}}async function tt(){try{await M("POST",`/api/v1/projects/${o.currentProjectId}/stacks`,{body:{env:o.currentEnv,name:c.value,specYaml:f.value},idempotent:!0}),x.success("应用栈拓扑与输出参数校验通过，已保存"),await z()}catch(s){x.error(String(s))}}async function et(s){try{const{data:t}=await M("POST",`/api/v1/projects/${o.currentProjectId}/stacks/${s.id}:apply`,{idempotent:!0});await o.refreshApprovalBadge(),t.state==="awaiting_approval"?(x.warning(`生产环境应用栈 ${s.name} 已进入变更审批队列`),await v.push("/operations/approvals")):(x.success(`应用栈分层部署任务 ${t.id} 已下发`),await v.push(`/operations?id=${t.id}`))}catch(t){x.error(String(t))}}function at(s){d.stack=s,d.open=!0}async function st(){if(d.stack){d.loading=!0;try{const{data:s}=await M("POST",`/api/v1/projects/${o.currentProjectId}/stacks/${d.stack.id}:uninstall`,{idempotent:!0});d.open=!1,await o.refreshApprovalBadge(),s.state==="awaiting_approval"?(x.warning("逆序卸载已进入变更审批队列"),await v.push("/operations/approvals")):(x.success(`逆序卸载操作 ${s.id} 已入队（外部引用已自动保护）`),await v.push(`/operations?id=${s.id}`))}catch(s){x.error(String(s))}finally{d.loading=!1}}}return ft(()=>o.currentProjectId,z),gt(z),(s,t)=>{var Y,H,U;const K=dt,N=rt,B=mt,nt=ut,ot=pt,it=kt;return l(),b("div",null,[i(ct,{title:"应用栈编排",refresh:z,refreshing:p.value},null,8,["refreshing"]),e("div",ee,[e("div",ae,[t[5]||(t[5]=e("span",null,[T("复合应用栈拓扑画布"),e("span",{class:"sub"},"实时依赖分层校验 · 自动阻断循环依赖 · 插件输出参数自动关联")],-1)),e("div",se,[i(K,{loading:k.value,onClick:$},{default:h(()=>[i(r(Tt),{size:14,class:"btn-ico"}),t[3]||(t[3]=T(" 校验并渲染拓扑 ",-1))]),_:1},8,["loading"]),r(u).can("stack:write",r(o).currentProjectId)?(l(),P(K,{key:0,type:"primary",onClick:tt},{default:h(()=>[i(r(jt),{size:14,class:"btn-ico"}),t[4]||(t[4]=T(" 校验并保存应用栈 ",-1))]),_:1})):S("",!0)])]),g.value?(l(),P(X,{key:0,variant:"danger",text:g.value,class:"ki-mb-3"},null,8,["text"])):S("",!0),a.value?(l(),b("div",ne,[t[6]||(t[6]=e("span",{class:"ki-secondary"},"安装执行分层：",-1)),(l(!0),b(D,null,R(a.value.installLayers,(m,V)=>(l(),b("span",{key:V,class:"ki-badge ki-mono layer-pill"}," L"+y(V)+" · "+y(m.join(", ")),1))),128)),(Y=a.value.externalRefIds)!=null&&Y.length?(l(),b("span",oe," 受保护外部引用："+y(a.value.externalRefIds.join(", ")),1)):S("",!0)])):S("",!0),i(te,{instances:E.value},null,8,["instances"])]),e("div",ie,[e("div",re,[e("div",le,[t[7]||(t[7]=e("span",null,"声明式 Stack YAML",-1)),i(N,{modelValue:c.value,"onUpdate:modelValue":t[0]||(t[0]=m=>c.value=m),size:"small",class:"ki-mono stack-name-input"},null,8,["modelValue"])]),e("div",ce,[(l(),b(D,null,R(w,(m,V)=>e("button",{key:V,type:"button",class:yt(["ki-pill-tab",{"ki-pill-tab--active":I.value===V}]),onClick:ve=>O(V)},y(m.label),11,de)),64))]),i(N,{modelValue:f.value,"onUpdate:modelValue":t[1]||(t[1]=m=>f.value=m),type:"textarea",rows:14,class:"ki-mono"},null,8,["modelValue"])]),e("div",ue,[e("div",pe,[e("span",null,[t[8]||(t[8]=T("已保存应用栈清单",-1)),e("span",me,"共 "+y(n.value.length)+" 套 · 点击行载入上方画布",1)])]),vt((l(),P(ot,{data:n.value,size:"small",onRowClick:Z},{empty:h(()=>[i(nt,{description:"暂无已保存应用栈，点击上方按钮保存当前模板","image-size":60})]),default:h(()=>[i(B,{prop:"name",label:"应用栈名称","min-width":"140"},{default:h(({row:m})=>[e("b",ke,y(m.name),1)]),_:1}),i(B,{label:"环境",width:"106"},{default:h(({row:m})=>[i(bt,{env:String(m.env||r(o).currentEnv)},null,8,["env"])]),_:1}),i(B,{label:"状态",width:"96"},{default:h(({row:m})=>[i(Q,{status:String(m.state||"ready")},null,8,["status"])]),_:1}),i(B,{label:"操作",width:"148"},{default:h(({row:m})=>[e("div",fe,[r(u).can("stack:write",r(o).currentProjectId)?(l(),P(K,{key:0,link:"",type:"primary",onClick:q(V=>et(m),["stop"])},{default:h(()=>[i(r(It),{size:13,class:"btn-ico"}),t[9]||(t[9]=T(" Apply ",-1))]),_:1},8,["onClick"])):S("",!0),r(u).can("stack:write",r(o).currentProjectId)?(l(),P(K,{key:1,link:"",type:"danger",onClick:q(V=>at(m),["stop"])},{default:h(()=>[i(r(Et),{size:13,class:"btn-ico"}),t[10]||(t[10]=T(" 逆序卸载 ",-1))]),_:1},8,["onClick"])):S("",!0)])]),_:1})]),_:1},8,["data"])),[[it,p.value]])])]),i(wt,{modelValue:d.open,"onUpdate:modelValue":t[2]||(t[2]=m=>d.open=m),env:String(((H=d.stack)==null?void 0:H.env)||r(o).currentEnv),title:"确认逆序卸载应用栈","resource-name":String(((U=d.stack)==null?void 0:U.name)||""),"risk-summary":"将严格按依赖逆序分层卸载栈内自建实例；externalRefs 声明的外部托管服务绝不会被修改或删除。","confirm-label":"解锁并逆序卸载",loading:d.loading,onConfirm:st},null,8,["modelValue","env","resource-name","loading"])])}}}),Ae=A(ge,[["__scopeId","data-v-b58498e6"]]);export{Ae as default};
