package qmi

import (
	"context"
	"encoding/binary"
	"fmt"
)

// ServiceVersion 表示 modem 上某个 QMI 服务的版本信息
type ServiceVersion struct {
	ServiceType uint8  // QMI 服务类型 (如 ServiceWDS, ServiceNAS 等)
	Major       uint16 // 主版本号
	Minor       uint16 // 次版本号
}

// GetServiceVersions 向 modem 发送 CTL_GET_VERSION_INFO 请求，
// 获取 modem 支持的全部 QMI 服务及其版本号。
//
// 响应 TLV 0x01 格式：
//   byte 0:     count (服务数量)
//   byte 1..N:  每个条目 5 字节：
//     uint8  service_type
//     uint16 major_version (LE)
//     uint16 minor_version (LE)
func (c *Client) GetServiceVersions(ctx context.Context) ([]ServiceVersion, error) {
	resp, err := c.SendRequest(ctx, ServiceControl, 0, CTLGetVersionInfo, nil)
	if err != nil {
		return nil, fmt.Errorf("CTL GET_VERSION_INFO 请求失败: %w", err)
	}
	if err := resp.CheckResult(); err != nil {
		return nil, fmt.Errorf("CTL GET_VERSION_INFO 响应错误: %w", err)
	}
	return parseServiceVersionList(resp.TLVs)
}

// parseServiceVersionList 从 TLV 0x01 解析服务版本列表
func parseServiceVersionList(tlvs []TLV) ([]ServiceVersion, error) {
	tlv := FindTLV(tlvs, 0x01)
	if tlv == nil {
		return nil, fmt.Errorf("响应中缺少服务版本列表 TLV (0x01)")
	}
	if len(tlv.Value) < 1 {
		return nil, fmt.Errorf("服务版本列表 TLV 数据过短")
	}

	count := int(tlv.Value[0])
	const entrySize = 5 // 1(service) + 2(major) + 2(minor)
	expected := 1 + count*entrySize
	if len(tlv.Value) < expected {
		return nil, fmt.Errorf("服务版本列表截断: 期望 %d 字节, 实际 %d", expected, len(tlv.Value))
	}

	versions := make([]ServiceVersion, count)
	for i := 0; i < count; i++ {
		offset := 1 + i*entrySize
		versions[i] = ServiceVersion{
			ServiceType: tlv.Value[offset],
			Major:       binary.LittleEndian.Uint16(tlv.Value[offset+1 : offset+3]),
			Minor:       binary.LittleEndian.Uint16(tlv.Value[offset+3 : offset+5]),
		}
	}
	return versions, nil
}

// ServiceVersionMap 将服务版本切片转换为按 ServiceType 索引的 map，方便快速查询
func ServiceVersionMap(versions []ServiceVersion) map[uint8]ServiceVersion {
	m := make(map[uint8]ServiceVersion, len(versions))
	for _, v := range versions {
		m[v.ServiceType] = v
	}
	return m
}
