# AIOpsAnalyzer
一个检测k8s集群中pod的资源使用情况，以及告警和日志，聚合后发送给大模型，获取处理建议以及可直接运行的patch，发送飞书审批请求，通过后发起合并请求推送到仓库，使用argocd获取git仓库，达到监控、分析、处理、审批、执行闭环。
手动更改svc的类型为nodeport，无法重新helm update，要--force
helm资源重启端口号变了，因为重置了
loki原生无自动加入label，导致label无法匹配，一直无法获取日志。
给grafana配置loki，如果loki开启了多租户（默认），要加对应的header   "X-Scope-OrgID: 1"
k8s初始化忘加端口号