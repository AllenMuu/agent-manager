# TDD 红绿证据

以下失败先于对应生产修改发生，均通过公开接口和真实临时文件系统复现。详细终端结果保存在本聊天工具记录，最终通过结果见 focused.log。

| 测试 | 修复前实测失败 | 最小修改后 |
| --- | --- | --- |
| TestInitializeRollbackPreservesUnpublishedConcurrentFile | concurrent file lost during rollback: no such file or directory | PASS |
| TestInitializeRollbackPreservesReplacedPublishedFile | concurrent replacement lost during rollback: no such file or directory | PASS |
| TestAddGitignoreTreatsManagedPathsLiterally | demo* / demo? 忽略相邻路径；demo[ab] / demo空格 未匹配自身 | PASS，真实 git check-ignore 验证 |
| TestAddGitignoreRejectsLineSeparatorsBeforeConfirmation | 换行及回车均 confirmed=true err=nil | PASS，拒绝前未确认、未写 .gitignore/日志目录 |
| TestLoadExpandsCurrentUserHomeRelativeLibrary | LibraryPath 是 configDir/~/.agents/skills | PASS，解析至当前用户 HOME |
| TestScanAttributesNestedMarkersToContainingScope | technologies=[go]，遗漏 compose/docker/postgresql | PASS |

补充通过测试：已发布文件同内容替换的身份保护、旧有内容恢复与日志不变、独立子作用域隔离、CLI PostgreSQL 推荐，以及上游已修复 R4/R5 的原始场景。
