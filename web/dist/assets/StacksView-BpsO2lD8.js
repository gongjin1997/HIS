import{E as oe}from"./index-BhunIpj1.js";import{_ as M,a as H,u as ie,j as re,E as le,k as j}from"./index-DwZg1PYM.js";import{E as ce,a as de,b as ue}from"./index-DuA0Mrto.js";import{v as pe}from"./directive-MPQbo2Bz.js";import{E as V}from"./index-BuEKkF3o.js";import{d as z,a as b,k as o,u as r,B as s,F as _,G as A,L as B,D as O,j as w,o as d,A as h,c as K,w as me,b as ke,T as x,a8 as fe,a1 as $,H as ge,ag as Y,ao as ve}from"./vue-vendor-Bg1EiT2j.js";import{K as F}from"./KiStatusBadge-DM54WJd0.js";import{K as ye}from"./KiEnvBadge-XssJ6LgX.js";import{K as W}from"./KiBanner-BUGEV1kr.js";import{K as he}from"./KiDangerConfirmModal-BALIbqLB.js";import{H as U,P as q,V as _e}from"./xyflow-DfJ8zBoL.js";import{K as be}from"./KiDomainTag-DlOdqOpK.js";import{B as Se,n as we,y as Ve,z as $e,D as Pe,u as Ce}from"./icons-D030hSmm.js";/* empty css                                                                       */import"./typescript--EWdIFjd.js";import"./index-BTS-U2D2.js";import"./xterm-pi3tdajZ.js";import"./omit-B8HqVbhN.js";import"./_baseIteratee-DN7qw0Fe.js";import"./index-CLmdzJwF.js";import"./KiButton-C6x1oI9o.js";function xe(p){const g=new Map;for(const u of p)g.set(u.id,u.dependsOn||[]);const i=new Map,k=[],v=u=>{i.set(u,1),k.push(u);for(const n of g.get(u)||[]){if(!g.has(n))continue;const f=i.get(n)||0;if(f===1){const l=k.indexOf(n);return[...k.slice(l),n]}if(f===0){const l=v(n);if(l)return l}}return k.pop(),i.set(u,2),null};for(const u of p)if((i.get(u.id)||0)===0){const n=v(u.id);if(n)return{hasCycle:!0,cyclePath:n}}return{hasCycle:!1,cyclePath:[]}}const Oe={class:"ki-dag-node ki-surface"},Te={class:"ki-dag-node__head"},je={class:"ki-flex-row ki-truncate"},Ie={class:"ki-mono ki-truncate"},Ee={class:"ki-dag-node__body"},De={class:"ki-flex-between"},Ke={class:"ki-dag-node__prod"},Ae={key:0,class:"ki-dag-ports"},Be={class:"ki-mono"},Me={class:"ki-dag-node__foot ki-muted"},ze=z({__name:"KiStackDagNode",props:{data:{}},setup(p){return(g,i)=>{var k;return d(),b("div",Oe,[o(r(U),{type:"target",position:r(q).Left,class:"ki-dag-handle"},null,8,["position"]),s("header",Te,[s("div",je,[o(r(Se),{size:13,class:"ki-dag-node__icon","aria-hidden":"true"}),s("strong",Ie,_(p.data.id),1)]),o(F,{status:p.data.status||"ready"},null,8,["status"])]),s("div",Ee,[s("div",De,[s("span",Ke,_(p.data.product)+" "+_(p.data.version),1),o(be,{kind:"target",value:p.data.target},null,8,["value"])]),p.data.outputs&&p.data.outputs.length?(d(),b("div",Ae,[(d(!0),b(A,null,B(p.data.outputs,v=>(d(),b("div",{key:v.name,class:"ki-dag-port"},[v.secret?(d(),O(r(we),{key:0,size:11,class:"ki-text-warn","aria-hidden":"true"})):w("",!0),s("span",Be,_(v.name),1)]))),128))])):w("",!0)]),s("footer",Me,[s("span",null,"上游依赖 "+_(p.data.dependsOnCount??0),1),s("span",null,"Output "+_(((k=p.data.outputs)==null?void 0:k.length)??0),1)]),o(r(U),{type:"source",position:r(q).Right,class:"ki-dag-handle"},null,8,["position"])])}}}),Ge=M(ze,[["__scopeId","data-v-edf7785c"]]),Le={class:"ki-dag-wrap"},Ne={class:"ki-dag-canvas ki-surface"},Re=z({__name:"StackCanvas",props:{instances:{}},setup(p){const g=p,i=H(),k=K(()=>xe(g.instances.map(n=>({id:n.id,dependsOn:n.dependsOn||[]})))),v=K(()=>{const n=new Map,f=(t,S=new Set)=>{if(n.has(t))return n.get(t);if(S.has(t))return 0;S.add(t);const y=g.instances.find(m=>m.id===t);if(!y||!y.dependsOn.length)return n.set(t,0),0;const P=1+Math.max(...y.dependsOn.map(m=>f(m,S)));return n.set(t,P),P};g.instances.forEach(t=>f(t.id));const l=new Map;return g.instances.map(t=>{const S=n.get(t.id)||0,y=l.get(S)||0;return l.set(S,y+1),{id:t.id,type:"kiStackNode",position:{x:36+S*300,y:36+y*148},data:{id:t.id,product:t.product,version:t.version,target:t.target,status:t.status||"ready",dependsOnCount:(t.dependsOn||[]).length,outputs:t.outputs||[{name:"endpoint"},{name:"rootPassword",secret:!0}]}}})}),u=K(()=>{const n=[];for(const f of g.instances)for(const l of f.dependsOn||[])n.push({id:`${l}->${f.id}`,source:l,target:f.id,animated:f.status==="running"});return n});return(n,f)=>(d(),b("div",Le,[k.value.hasCycle?(d(),O(W,{key:0,variant:"danger",text:`检测到 Stack DAG 循环依赖：${k.value.cyclePath.join(" → ")}，已阻断部署计划生成`,class:"ki-mb-3"},null,8,["text"])):w("",!0),s("div",Ne,[o(r(_e),{nodes:v.value,edges:u.value,"color-mode":r(i).theme,"fit-view-on-init":""},{"node-kiStackNode":h(l=>[o(Ge,{data:l.data},null,8,["data"])]),_:1},8,["nodes","edges","color-mode"])])]))}}),Ye=M(Re,[["__scopeId","data-v-5ab1506e"]]),Ue={class:"ki-card ki-mb-4"},qe={class:"ki-card__title"},He={class:"ki-flex-row"},Fe={key:1,class:"dag-layers ki-mb-3"},We={key:0,class:"ki-badge ki-mono ext-pill"},Je={class:"ki-grid-2"},Qe={class:"ki-card"},Xe={class:"ki-card__title"},Ze={class:"preset-bar ki-mb-3"},et={class:"ki-card"},tt={class:"ki-card__title"},at={class:"sub"},st={class:"ki-mono"},nt=z({__name:"StacksView",setup(p){const g=ie(),i=H(),k=ve(),v={logging:{label:"P3 日志分析栈 (S3+Kafka+OS+DataPrepper)",name:"prod-t3-logging-platform",yaml:`apiVersion: kinstall.io/v1alpha1
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
`},hisCore:{label:"P4 核心交易栈 (PG+Valkey+Kafka)",name:"his-core-app-stack",yaml:`apiVersion: kinstall.io/v1alpha1
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
`},devops:{label:"P8 研发治理栈 (PG+Valkey+GitLab+Harbor)",name:"devops-platform-stack",yaml:`apiVersion: kinstall.io/v1alpha1
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
`}},u=$(!1),n=$(!1),f=$([]),l=$(v.logging.name),t=$(v.logging.yaml),S=$([]),y=$(null),P=$(""),m=ge({open:!1,stack:null,loading:!1});async function I(){if(!(!i.currentProjectId||!t.value.trim())){n.value=!0,P.value="";try{const{data:a}=await j("POST",`/api/v1/projects/${i.currentProjectId}/stacks:validate`,{body:{specYaml:t.value}});y.value=a.dag,S.value=(a.nodes||[]).map(e=>({...e,status:"ready"}))}catch(a){P.value=String(a),y.value=null}finally{n.value=!1}}}function J(a){const e=v[a];e&&(l.value=e.name,t.value=e.yaml,I())}function Q(a){l.value=String(a.name||""),typeof a.specYaml=="string"&&a.specYaml.trim()&&(t.value=a.specYaml,I())}async function E(){if(i.currentProjectId){u.value=!0;try{const{data:a}=await j("GET",`/api/v1/projects/${i.currentProjectId}/stacks`);f.value=a.items||[],await I()}finally{u.value=!1}}}async function X(){try{await j("POST",`/api/v1/projects/${i.currentProjectId}/stacks`,{body:{env:i.currentEnv,name:l.value,specYaml:t.value},idempotent:!0}),V.success("Stack DAG 拓扑排序与 OutputSchema 静态校验通过并已保存"),await E()}catch(a){V.error(String(a))}}async function Z(a){try{const{data:e}=await j("POST",`/api/v1/projects/${i.currentProjectId}/stacks/${a.id}:apply`,{idempotent:!0});await i.refreshApprovalBadge(),e.state==="awaiting_approval"?(V.warning(`生产环境 Stack ${a.name} 已进入四眼审批队列`),await k.push("/operations/approvals")):(V.success(`Stack 分层编排操作 ${e.id} 已下发`),await k.push(`/operations?id=${e.id}`))}catch(e){V.error(String(e))}}function ee(a){m.stack=a,m.open=!0}async function te(){if(m.stack){m.loading=!0;try{const{data:a}=await j("POST",`/api/v1/projects/${i.currentProjectId}/stacks/${m.stack.id}:uninstall`,{idempotent:!0});m.open=!1,await i.refreshApprovalBadge(),a.state==="awaiting_approval"?(V.warning("逆序卸载已进入四眼审批队列"),await k.push("/operations/approvals")):(V.success(`逆序卸载操作 ${a.id} 已入队（externalRefs 已自动保护）`),await k.push(`/operations?id=${a.id}`))}catch(a){V.error(String(a))}finally{m.loading=!1}}}return me(()=>i.currentProjectId,E),ke(E),(a,e)=>{var L,N,R;const T=le,G=oe,D=de,ae=ue,se=ce,ne=pe;return d(),b("div",null,[o(re,{title:"Stack DAG 编排",refresh:E,refreshing:u.value},null,8,["refreshing"]),s("div",Ue,[s("div",qe,[e[5]||(e[5]=s("span",null,[x("复合栈实时 DAG 画布"),s("span",{class:"sub"},"后端 Kahn 拓扑排序 + 环路阻断 (KI-STACK-0001) + 真实插件 OutputSchema 句柄")],-1)),s("div",He,[o(T,{loading:n.value,onClick:I},{default:h(()=>[o(r(Ve),{size:14,class:"btn-ico"}),e[3]||(e[3]=x(" 实时校验并渲染 DAG ",-1))]),_:1},8,["loading"]),r(g).can("stack:write",r(i).currentProjectId)?(d(),O(T,{key:0,type:"primary",onClick:X},{default:h(()=>[o(r($e),{size:14,class:"btn-ico"}),e[4]||(e[4]=x(" 校验并保存 Stack ",-1))]),_:1})):w("",!0)])]),P.value?(d(),O(W,{key:0,variant:"danger",text:P.value,class:"ki-mb-3"},null,8,["text"])):w("",!0),y.value?(d(),b("div",Fe,[e[6]||(e[6]=s("span",{class:"ki-secondary"},"安装分层：",-1)),(d(!0),b(A,null,B(y.value.installLayers,(c,C)=>(d(),b("span",{key:C,class:"ki-badge ki-mono layer-pill"}," L"+_(C)+": "+_(c.join(", ")),1))),128)),(L=y.value.externalRefIds)!=null&&L.length?(d(),b("span",We," 受保护 externalRefs: "+_(y.value.externalRefIds.join(", ")),1)):w("",!0)])):w("",!0),o(Ye,{instances:S.value},null,8,["instances"])]),s("div",Je,[s("div",Qe,[s("div",Xe,[e[7]||(e[7]=s("span",null,"声明式 Stack YAML",-1)),o(G,{modelValue:l.value,"onUpdate:modelValue":e[0]||(e[0]=c=>l.value=c),size:"small",class:"ki-mono stack-name-input"},null,8,["modelValue"])]),s("div",Ze,[(d(),b(A,null,B(v,(c,C)=>o(T,{key:C,size:"small",plain:"",onClick:ot=>J(C)},{default:h(()=>[x(_(c.label),1)]),_:2},1032,["onClick"])),64))]),o(G,{modelValue:t.value,"onUpdate:modelValue":e[1]||(e[1]=c=>t.value=c),type:"textarea",rows:14,class:"ki-mono"},null,8,["modelValue"])]),s("div",et,[s("div",tt,[s("span",null,[e[8]||(e[8]=x("已注册 Stack 清单",-1)),s("span",at,"共 "+_(f.value.length)+" 个 · 点击行可在画布加载 DAG",1)])]),fe((d(),O(se,{data:f.value,size:"small",onRowClick:Q},{empty:h(()=>[o(ae,{description:"暂无已保存 Stack，点击上方按钮保存当前模板","image-size":60})]),default:h(()=>[o(D,{prop:"name",label:"名称","min-width":"150"},{default:h(({row:c})=>[s("b",st,_(c.name),1)]),_:1}),o(D,{label:"环境",width:"95"},{default:h(({row:c})=>[o(ye,{env:String(c.env||r(i).currentEnv)},null,8,["env"])]),_:1}),o(D,{label:"状态",width:"95"},{default:h(({row:c})=>[o(F,{status:String(c.state||"ready")},null,8,["status"])]),_:1}),o(D,{label:"操作",width:"145",fixed:"right"},{default:h(({row:c})=>[r(g).can("stack:write",r(i).currentProjectId)?(d(),O(T,{key:0,link:"",type:"primary",onClick:Y(C=>Z(c),["stop"])},{default:h(()=>[o(r(Pe),{size:13,class:"btn-ico"}),e[9]||(e[9]=x(" Apply ",-1))]),_:1},8,["onClick"])):w("",!0),r(g).can("stack:write",r(i).currentProjectId)?(d(),O(T,{key:1,link:"",type:"danger",onClick:Y(C=>ee(c),["stop"])},{default:h(()=>[o(r(Ce),{size:13,class:"btn-ico"}),e[10]||(e[10]=x(" 逆序卸载 ",-1))]),_:1},8,["onClick"])):w("",!0)]),_:1})]),_:1},8,["data"])),[[ne,u.value]])])]),o(he,{modelValue:m.open,"onUpdate:modelValue":e[2]||(e[2]=c=>m.open=c),env:String(((N=m.stack)==null?void 0:N.env)||r(i).currentEnv),title:"确认逆序卸载 Stack 复合栈","resource-name":String(((R=m.stack)==null?void 0:R.name)||""),"risk-summary":"将严格按照反向拓扑分层 (UninstallLayers) 逐层卸载栈内自建实例；externalRefs 声明的外部云服务绝不会被触碰。","confirm-label":"解锁并逆序卸载",loading:m.loading,onConfirm:te},null,8,["modelValue","env","resource-name","loading"])])}}}),Ct=M(nt,[["__scopeId","data-v-9a7e9fe5"]]);export{Ct as default};
