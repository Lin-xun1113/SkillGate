# 许可证与依赖检查

**状态：** `IMPLEMENTED`
**版本：** v0.2（2026-08-30 UTC）

## 项目许可证

项目许可证已由所有者确认并采用 MIT；版权主体为 `Lin-xun1113`，见仓库根目录 [`LICENSE`](../../LICENSE)。

运行检查：

```bash
bash scripts/check-licenses.sh
```

该命令仍会报告第三方依赖工具是否已安装；依赖工具缺失时只表示本地扫描未执行，不改变项目 MIT 许可证状态。

## 第三方依赖

依赖检查不等同于项目许可证确认。个人项目暂不接入 CI；需要时在本地受控环境运行并保存原始报告：

```bash
# Go（工具需预先安装，避免运行时联网下载）
go-licenses csv ./...

# Node（使用 lockfile；工具需预先安装）
npx --no-install license-checker --production --summary

# Python（使用锁定 requirements；工具需预先安装）
python -m piplicenses --from=mixed --format=csv
```

报告必须标明扫描时间（UTC）、Lockfile/Requirements Hash、工具版本和未识别许可证；不能把扫描结果写入 Manifest、Artifact 或生产 Trace。
