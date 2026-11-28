import{E as S,a as b}from"./index-B0JEQUA1.js";import{E as $,_ as D}from"./_plugin-vue_export-helper-CaTiXzxo.js";import{E as N}from"./index-BA6uW80v.js";import{E as O}from"./index-D6Uj-3Fb.js";import{E as G}from"./index-DVqJJPxm.js";import{f as C}from"./index-A5F7t971.js";import{d as I,s as U,b as y,f as t,y as o,z as l,F as M,J as h,a2 as c,c as F,o as m,u as d,T as i,n as q,D as n,B as H,e as J,af as X}from"./vue-vendor-CB5-E4hF.js";import{P as j}from"./PageHeader-CvYGvZQr.js";import{l as K,B as Q,x as W,R as Z,n as ee}from"./icons-BkR-VU_q.js";import"./raf-8Tyv1qb4.js";import"./index-Dc6DSbyU.js";import"./refs-D6FW0B1r.js";import"./xterm-C4n0e05E.js";const ae={class:"plugins-page"},le={class:"ki-pill-tabs cat-bar"},te=["onClick"],se={class:"plg-grid"},oe={class:"plg-head"},ie={class:"plg-title"},ne={class:"ki-mono ki-muted"},re={class:"plg-desc"},ue={class:"plg-meta ki-muted"},ce={class:"plg-foot"},de=`apiVersion: kinstall.io/v2
code: custom-service
name: 自定义业务微服务插件
category: application
version: "1.0.0"
runtime: compose
cmdbModel: m_app
defaultPort: 8088
description: 通过 YAML Manifest 声明参数 Schema、安装脚本与健康拨测，无需编写 Go 代码
params:
  - key: APP_PORT
    label: 监听端口
    type: int
    default: "8088"
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "2"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "4"
installScript: |
  echo "[Plugin:custom-service] Deploying {{SERVICE_NAME}} on port {{APP_PORT}}"
healthCheck:
  type: http
  port: "{{APP_PORT}}"
  path: /healthz
uninstallScript: |
  echo "[Plugin:custom-service] Removing {{SERVICE_NAME}}"
`,pe=I({__name:"PluginsView",setup(me){const V=X(),g=c(!1),_=c([]),v=c("all"),r=c(!1),k=c("新建声明式 YAML 插件包"),p=c(""),A=[{key:"all",label:"全部插件"},{key:"database",label:"数据库"},{key:"middleware",label:"中间件与网关"},{key:"storage",label:"对象存储"},{key:"application",label:"业务应用"},{key:"observability",label:"监控采集"}],w=F(()=>v.value==="all"?_.value:_.value.filter(s=>s.category===v.value));async function f(){g.value=!0;try{const{data:s}=await C("GET","/api/v1/delivery/plugins");_.value=s.items||[]}finally{g.value=!1}}function B(){k.value="新建声明式 YAML 插件包",p.value=de,r.value=!0}function R(s){k.value=`编辑插件声明 · ${s.name}`,p.value=s.manifestYaml,r.value=!0}async function z(){try{await C("POST","/api/v1/delivery/plugins",{body:{manifestYaml:p.value}}),b.success("声明式 YAML 插件已保存生效"),r.value=!1,await f()}catch(s){b.error(s instanceof Error?s.message:"YAML 校验失败")}}async function L(s){await G.confirm(`确认删除插件「${s.name}」？`,"删除插件",{type:"warning"}),await C("DELETE",`/api/v1/delivery/plugins/${s.code}`),b.success("插件已删除"),await f()}return U(f),(s,a)=>{const u=$,T=N,Y=S,x=O;return m(),y("div",ae,[t(j,{title:"声明式 YAML 插件库",subtitle:"参考 1Panel / LiveOps IaC · 零 Go 代码扩展组件与业务应用包",refresh:f,refreshing:g.value},{actions:o(()=>[t(u,{type:"primary",onClick:B},{default:o(()=>[t(d(K),{size:14,class:"btn-ico"}),a[3]||(a[3]=i("新建 YAML 插件",-1))]),_:1})]),_:1},8,["refreshing"]),l("div",le,[(m(),y(M,null,h(A,e=>l("button",{key:e.key,type:"button",class:q(["ki-pill-tab",{"ki-pill-tab--active":v.value===e.key}]),onClick:E=>v.value=e.key},n(e.label),11,te)),64))]),l("div",se,[(m(!0),y(M,null,h(w.value,e=>{var E;return m(),y("article",{key:e.id,class:"ki-card plg-card"},[l("div",oe,[l("div",ie,[t(d(Q),{size:18,class:"plg-ico"}),l("div",null,[l("h3",null,n(e.name),1),l("span",ne,n(e.code)+" · v"+n(e.version),1)])]),t(T,{size:"small"},{default:o(()=>[i(n(e.runtime),1)]),_:2},1024)]),l("p",re,n(e.description),1),l("div",ue,[l("span",null,"分类: "+n(e.category),1),l("span",null,"参数项: "+n(((E=e.schema)==null?void 0:E.length)||0)+" 个",1),l("span",null,n(e.isBuiltin?"官方内置":"自定义扩展"),1)]),l("div",ce,[t(u,{size:"small",onClick:P=>R(e)},{default:o(()=>[t(d(W),{size:13,class:"btn-ico"}),a[4]||(a[4]=i("YAML 源码",-1))]),_:1},8,["onClick"]),t(u,{size:"small",type:"primary",plain:"",onClick:P=>d(V).push(`/delivery?plugin=${e.code}`)},{default:o(()=>[t(d(Z),{size:13,class:"btn-ico"}),a[5]||(a[5]=i("发起编排 ",-1))]),_:1},8,["onClick"]),e.isBuiltin?J("",!0):(m(),H(u,{key:0,link:"",type:"danger",size:"small",onClick:P=>L(e)},{default:o(()=>[t(d(ee),{size:13})]),_:1},8,["onClick"]))])])}),128))]),t(x,{modelValue:r.value,"onUpdate:modelValue":a[2]||(a[2]=e=>r.value=e),title:k.value,width:"760px"},{footer:o(()=>[t(u,{onClick:a[1]||(a[1]=e=>r.value=!1)},{default:o(()=>[...a[6]||(a[6]=[i("取消",-1)])]),_:1}),t(u,{type:"primary",onClick:z},{default:o(()=>[...a[7]||(a[7]=[i("校验并保存插件",-1)])]),_:1})]),default:o(()=>[a[8]||(a[8]=l("p",{class:"ki-muted yaml-tip"},[i(" 支持参数模板变量（如 "),l("code",{class:"ki-mono"},"{{SERVICE_NAME}}"),i("、"),l("code",{class:"ki-mono"},"{{PG_PORT}}"),i("），发布时由三级参数引擎自动渲染并同步登记 CMDB 资产。 ")],-1)),t(Y,{modelValue:p.value,"onUpdate:modelValue":a[0]||(a[0]=e=>p.value=e),type:"textarea",rows:20,class:"yaml-area"},null,8,["modelValue"])]),_:1},8,["modelValue","title"])])}}}),Ae=D(pe,[["__scopeId","data-v-c09db325"]]);export{Ae as default};
