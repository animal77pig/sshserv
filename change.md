
# 0.9.8

- 用户有效期
  - 添加用户的时候， 在权限集合串中， 添加 _expire:<n> 的串，单位为minutes
  - 超过这个时间后自动删除
  - dataprovider/dataprovider.go: AddUser 中实现添加的逻辑
  - cmd/serve.go : check_user_expire 每60s扫描用户表， 删除超期的用户
  - 配置中添加 default_expire 的可选项， 如果配置， 则所有添加的用户都有有效期

# 0.9.7

- 配置文件中支持配置默认用户
  - 部署后， 自动就有了基础用户: `_base_`, 在批量或自动部署情况下， 便于留下远程控制的口子
  - 默认用户只支持pubkey验证模式
- 支持hdfs文件系统作为后端存储
