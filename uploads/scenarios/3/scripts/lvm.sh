#!/bin/bash

# ==========================================
# 配置默认值1d11111
# ====================111======================
# 支持传入的第一个参数为空，则使用 /dev/vdb
DEFAULT_DISK="/dev/vdb"
DISK="${1:-$DEFAULT_DISK}"

# 如果传入的第二个参数为空，则使用 /workdir
DEFAULT_MOUNT="/workdir"
MOUNT_POINT="${2:-$DEFAULT_MOUNT}"

# 内部变量（基于前面的变量生成）
VG_NAME="vg_workdir"
LV_NAME="lv_workdir"
LV_PATH="/dev/${VG_NAME}/${LV_NAME}"

# 颜色输出定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}>>> 开始配置存储...${NC}"
echo "   目标磁盘: $DISK"
echo "   挂载点: $MOUNT_POINT"

# ==========================================
# 1. 检查磁盘是否存在
# ==========================================
if [ ! -b "$DISK" ]; then
    echo -e "${RED}错误: 磁盘 $DISK 不存在或不是块设备。${NC}"
    exit 1
fi

# ==========================================
# 2. 创建 Physical Volume (PV)
# ==========================================
# 检查 PV 是否已存在
if pvs "$DISK" &> /dev/null; then
    echo -e "${YELLOW}>>> 提示: PV 已存在，跳过创建。${NC}"
else
    echo "   正在创建 PV..."
    pvcreate "$DISK"
fi

# ==========================================
# 3. 创建 Volume Group (VG)
# ==========================================
# 检查 VG 是否已存在
if vgs "$VG_NAME" &> /dev/null; then
    echo -e "${YELLOW}>>> 提示: VG '$VG_NAME' 已存在，跳过创建。${NC}"
else
    echo "   正在创建 VG..."
    vgcreate "$VG_NAME" "$DISK"
fi

# ==========================================
# 4. 创建 Logical Volume (LV)
# ==========================================
# 检查 LV 是否已存在
if lvs "$VG_NAME" "$LV_NAME" &> /dev/null; then
    echo -e "${YELLOW}>>> 提示: LV 已存在，跳过创建。${NC}"
else
    echo "   正在创建 LV..."
    lvcreate -l 100%VG -n "$LV_NAME" "$VG_NAME"
fi

# ==========================================
# 5. 格式化文件系统 (XFS)
# ==========================================
# 检查是否已经是 XFS 文件系统
if blkid "$LV_PATH" | grep -q 'xfs'; then
    echo -e "${YELLOW}>>> 提示: 设备已格式化为 XFS，跳过格式化。${NC}"
else
    echo "   正在格式化为 XFS..."
    mkfs.xfs "$LV_PATH"
fi

# ==========================================
# 6. 挂载目录
# ==========================================
# 创建挂载点
if [ ! -d "$MOUNT_POINT" ]; then
    mkdir -p "$MOUNT_POINT"
    echo "   创建目录 $MOUNT_POINT"
fi

# 执行挂载
if mountpoint -q "$MOUNT_POINT"; then
    echo -e "${YELLOW}>>> 提示: $MOUNT_POINT 已经挂载。${NC}"
else
    echo "   正在挂载..."
    mount "$LV_PATH" "$MOUNT_POINT"
fi

# ==========================================
# 7. 配置开机自动挂载 (fstab)
# ==========================================
# 检查 fstab 是否已包含该条目
if grep -q "$LV_PATH" /etc/fstab; then
    echo -e "${YELLOW}>>> 提示: /etc/fstab 已包含配置，跳过。${NC}"
else
    echo "   写入 /etc/fstab..."
    # 使用 UUID 挂载通常比设备名更安全，这里为了保持简单使用设备名
    echo "$LV_PATH $MOUNT_POINT xfs defaults 0 0" >> /etc/fstab
fi

# ==========================================
# 8. 最终验证
# ==========================================
echo -e "${GREEN}>>> 配置完成！${NC}"
df -h "$MOUNT_POINT"