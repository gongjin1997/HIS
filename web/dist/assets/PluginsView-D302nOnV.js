import{E as R}from"./index-Bb15p4zu.js";import{a as D,b as B,c as b,_ as U}from"./index-jef4IBRk.js";import{E as G}from"./index-kh6A76BN.js";import{E as F}from"./index-Cb9HRKQ_.js";import{E as q,a as H}from"./index-EI9EZ5SC.js";import{E as J,a as O}from"./index-DIfOawqV.js";import{v as W}from"./directive-BEcnKg63.js";import{P as X,E as k}from"./PageHeader-1EhF0Ryu.js";import{E as K}from"./index-Cw-a7pRb.js";import{d as Q,w as Z,m as ee,a8 as te,a as m,g as a,f as l,b as o,a3 as y,J as ae,u as p,T as d,F as P,M as V,I as n,ao as se,o as c}from"./vue-vendor-CyxPdArc.js";import{R as le,w as oe,P as ie,ab as ne,x as re}from"./icons-BZtIVrN8.js";import"./typescript-DHinvwlV.js";import"./index-1T3buIQz.js";import"./xterm-pi3tdajZ.js";import"./vnode-PggotZ8R.js";import"./_baseIteratee-DAXSoH8n.js";import"./scroll-D7azLV7n.js";import"./validator-CbPezKOO.js";import"./omit-DIvxT-3R.js";import"./index-C9KPzF3k.js";const de={class:"ki-card"},ue={class:"plug-grid"},me={class:"plug-card__head"},pe={class:"ki-flex-row"},ce={class:"ki-badge ki-mono ver-pill"},fe={class:"ki-mono ki-muted plug-name"},ge={class:"ki-muted plug-desc"},ve={class:"plug-targets"},_e={class:"plug-foot"},be={class:"ki-flex-row"},ke={class:"ki-mono"},ye={class:"ki-mono"},Ee={class:"ki-flex-row ki-mb-2",style:{"justify-content":"space-between"}},$=`apiVersion: kinstall.io/v1alpha1
kind: PluginPackage
name: custom-app-service
displayName: 自定义业务服务容器包
category: 业务应用
version: 1.0.0
icon: Package
description: 通过声明式 YAML 定义参数 Schema、Docker Compose 模板与健康探针，无需修改任何 Go 代码
targets:
  - host-container
params:
  title: 自定义业务服务参数
  properties:
    image:
      type: string
      default: nginx:1.27-alpine
      x-kinstall-ui: { group: basic, order: 1, label: 容器镜像 }
    port:
      type: integer
      default: 8089
      x-kinstall-ui: { group: network, order: 2, label: 服务端口 }
composeTemplate: |
  services:
    app:
      image: "{{ .image }}"
      container_name: "kinstall-{{ .InstanceName }}"
      restart: unless-stopped
      ports:
        - "{{ .port }}:80"
healthCheck:
  tcpPort: 8089
  timeout: 30s
`,we=Q({__name:"PluginsView",setup(Pe){const f=D(),x=se(),v=y(!1),C=y("declarative"),E=y([]),w=y([]),s=ae({open:!1,title:"新建声明式插件包 (YAML)",manifestYaml:$,isEnabled:!0,saving:!1});async function g(){if(f.currentProjectId){v.value=!0;try{const[i,e]=await Promise.all([b("GET",`/api/v1/projects/${f.currentProjectId}/plugins/declarative`),b("GET","/api/v1/plugins")]);E.value=i.data.items||[],w.value=e.data.items||[]}finally{v.value=!1}}}function M(){s.title="新建声明式插件包 (YAML Manifest)",s.manifestYaml=$,s.isEnabled=!0,s.open=!0}function h(i){s.title=`编辑声明式插件包 · ${i.displayName}`,s.manifestYaml=i.manifestYaml,s.isEnabled=i.isEnabled,s.open=!0}async function T(){s.saving=!0;try{const{data:i}=await b("POST",`/api/v1/projects/${f.currentProjectId}/plugins/declarative`,{body:{manifestYaml:s.manifestYaml,isEnabled:s.isEnabled}});k.success(`声明式插件「${i.displayName} (${i.name})」已保存并热注册至执行引擎！`),s.open=!1,await g()}catch(i){k.error(String(i))}finally{s.saving=!1}}async function z(i){try{await K.confirm(`确认删除声明式插件「${i.displayName}」吗？`,"删除插件",{type:"warning"}),await b("DELETE",`/api/v1/projects/${f.currentProjectId}/plugins/declarative/${encodeURIComponent(i.id)}`),k.success("插件包已删除并从引擎卸载"),await g()}catch(e){e!=="cancel"&&String(e)!=="cancel"&&k.error(String(e))}}return Z(()=>f.currentProjectId,g),ee(g),(i,e)=>{const u=B,Y=O,_=H,I=q,L=J,N=F,j=R,A=G,S=W;return te((c(),m("div",null,[a(X,{title:"声明式插件与模板市场",subtitle:"YAML Manifest 声明业务应用/中间件模板与探针，保存即热注册生效",refresh:g,refreshing:v.value},{actions:l(()=>[a(u,{onClick:e[0]||(e[0]=t=>p(x).push("/delivery"))},{default:l(()=>[a(p(le),{size:14,class:"btn-ico"}),e[7]||(e[7]=d(" 前往分批交付 ",-1))]),_:1}),a(u,{type:"primary",onClick:M},{default:l(()=>[a(p(oe),{size:14,class:"btn-ico"}),e[8]||(e[8]=d(" 新建 YAML 插件包 ",-1))]),_:1})]),_:1},8,["refreshing"]),o("div",de,[a(L,{modelValue:C.value,"onUpdate:modelValue":e[2]||(e[2]=t=>C.value=t)},{default:l(()=>[a(Y,{label:`声明式可扩展插件包 (${E.value.length})`,name:"declarative"},{default:l(()=>[o("div",ue,[(c(!0),m(P,null,V(E.value,t=>(c(),m("div",{key:t.id,class:"plug-card"},[o("div",me,[o("div",pe,[a(p(ie),{size:16,class:"plug-ico"}),o("b",null,n(t.displayName),1)]),o("span",ce,"v"+n(t.version),1)]),o("div",fe,n(t.name)+" · "+n(t.category),1),o("p",ge,n(t.description),1),o("div",ve,[(c(!0),m(P,null,V(t.targets,r=>(c(),m("span",{key:r,class:"ki-badge target-pill ki-mono"},n(r),1))),128))]),o("div",_e,[a(u,{size:"small",onClick:r=>h(t)},{default:l(()=>[a(p(ne),{size:13,class:"btn-ico"}),e[9]||(e[9]=d(" 编辑 YAML ",-1))]),_:1},8,["onClick"]),o("div",be,[a(u,{type:"primary",size:"small",onClick:e[1]||(e[1]=r=>p(x).push("/instances"))},{default:l(()=>[...e[10]||(e[10]=[d("去部署",-1)])]),_:1}),a(u,{link:"",type:"danger",size:"small",onClick:r=>z(t)},{default:l(()=>[a(p(re),{size:13})]),_:1},8,["onClick"])])])]))),128))])]),_:1},8,["label"]),a(Y,{label:`内置核心中间件引擎 (${w.value.length})`,name:"builtin"},{default:l(()=>[a(I,{data:w.value,size:"small"},{default:l(()=>[a(_,{prop:"name",label:"引擎插件",width:"160"},{default:l(({row:t})=>[o("b",ke,n(t.name),1)]),_:1}),a(_,{prop:"version",label:"版本",width:"90"},{default:l(({row:t})=>[o("span",ye,n(t.version),1)]),_:1}),a(_,{label:"涵盖组件","min-width":"200"},{default:l(({row:t})=>[(c(!0),m(P,null,V(t.products||[],r=>(c(),m("span",{key:r.product,class:"ki-badge target-pill ki-mono"},n(r.product),1))),128))]),_:1}),a(_,{prop:"description",label:"能力说明","min-width":"260"})]),_:1},8,["data"])]),_:1},8,["label"])]),_:1},8,["modelValue"])]),a(A,{modelValue:s.open,"onUpdate:modelValue":e[6]||(e[6]=t=>s.open=t),title:s.title,width:"740px"},{footer:l(()=>[a(u,{onClick:e[5]||(e[5]=t=>s.open=!1)},{default:l(()=>[...e[12]||(e[12]=[d("取消",-1)])]),_:1}),a(u,{type:"primary",loading:s.saving,onClick:T},{default:l(()=>[...e[13]||(e[13]=[d("校验 YAML 并热注册",-1)])]),_:1},8,["loading"])]),default:l(()=>[o("div",Ee,[e[11]||(e[11]=o("span",{class:"ki-muted",style:{"font-size":"var(--ki-fs-xs)"}},[d("支持 Go 模板变量："),o("code",null,"{{ .InstanceName }}"),d(" 及自定义 params 字段；保存后立即热注入部署引擎。")],-1)),a(N,{modelValue:s.isEnabled,"onUpdate:modelValue":e[3]||(e[3]=t=>s.isEnabled=t),"active-text":"立即热注册启用"},null,8,["modelValue"])]),a(j,{modelValue:s.manifestYaml,"onUpdate:modelValue":e[4]||(e[4]=t=>s.manifestYaml=t),type:"textarea",rows:20,class:"yaml-editor ki-mono"},null,8,["modelValue"])]),_:1},8,["modelValue","title"])])),[[S,v.value]])}}}),Fe=U(we,[["__scopeId","data-v-86d7f0a8"]]);export{Fe as default};
