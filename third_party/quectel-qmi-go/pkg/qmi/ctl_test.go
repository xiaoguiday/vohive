package qmi

import (
	"encoding/binary"
	"testing"
)

// 构造标准的服务版本列表 TLV 0x01 测试数据
func buildVersionInfoTLV(services []ServiceVersion) []byte {
	const entrySize = 5 // 1(service) + 2(major) + 2(minor)
	data := make([]byte, 1+len(services)*entrySize)
	data[0] = byte(len(services))
	for i, svc := range services {
		offset := 1 + i*entrySize
		data[offset] = svc.ServiceType
		binary.LittleEndian.PutUint16(data[offset+1:offset+3], svc.Major)
		binary.LittleEndian.PutUint16(data[offset+3:offset+5], svc.Minor)
	}
	return data
}

func TestParseServiceVersionListNormal(t *testing.T) {
	// 构造 3 个服务：WDS v1.30, DMS v1.14, NAS v1.20
	input := []ServiceVersion{
		{ServiceType: ServiceWDS, Major: 1, Minor: 30},
		{ServiceType: ServiceDMS, Major: 1, Minor: 14},
		{ServiceType: ServiceNAS, Major: 1, Minor: 20},
	}
	tlvData := buildVersionInfoTLV(input)
	tlvs := []TLV{{Type: 0x01, Value: tlvData}}

	versions, err := parseServiceVersionList(tlvs)
	if err != nil {
		t.Fatalf("parseServiceVersionList() error = %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}

	// 验证每个服务
	for i, want := range input {
		got := versions[i]
		if got.ServiceType != want.ServiceType || got.Major != want.Major || got.Minor != want.Minor {
			t.Errorf("versions[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestParseServiceVersionListEmpty(t *testing.T) {
	// count=0 的合法响应
	tlvData := []byte{0x00}
	tlvs := []TLV{{Type: 0x01, Value: tlvData}}

	versions, err := parseServiceVersionList(tlvs)
	if err != nil {
		t.Fatalf("parseServiceVersionList() error = %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("got %d versions, want 0", len(versions))
	}
}

func TestParseServiceVersionListMissingTLV(t *testing.T) {
	// 没有 TLV 0x01
	_, err := parseServiceVersionList(nil)
	if err == nil {
		t.Fatal("expected error for nil TLVs")
	}

	// 有 TLV 但不是 0x01
	_, err = parseServiceVersionList([]TLV{{Type: 0x02, Value: []byte{0x00, 0x00, 0x00, 0x00}}})
	if err == nil {
		t.Fatal("expected error for missing TLV 0x01")
	}
}

func TestParseServiceVersionListTruncated(t *testing.T) {
	// count=2 但只提供 1 个条目的数据 (期望 11 字节，只有 6 字节)
	data := []byte{0x02, ServiceWDS, 0x01, 0x00, 0x1E, 0x00}
	tlvs := []TLV{{Type: 0x01, Value: data}}
	_, err := parseServiceVersionList(tlvs)
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestParseServiceVersionListTLVTooShort(t *testing.T) {
	// TLV 值为空
	tlvs := []TLV{{Type: 0x01, Value: []byte{}}}
	_, err := parseServiceVersionList(tlvs)
	if err == nil {
		t.Fatal("expected error for empty TLV value")
	}
}

func TestServiceVersionMap(t *testing.T) {
	versions := []ServiceVersion{
		{ServiceType: ServiceWDS, Major: 1, Minor: 30},
		{ServiceType: ServiceUIM, Major: 1, Minor: 46},
		{ServiceType: ServiceNAS, Major: 1, Minor: 20},
	}
	m := ServiceVersionMap(versions)

	// 应该能找到的
	if v, ok := m[ServiceWDS]; !ok {
		t.Fatal("WDS not found in map")
	} else if v.Major != 1 || v.Minor != 30 {
		t.Errorf("WDS version = %+v, want {1, 30}", v)
	}
	if _, ok := m[ServiceUIM]; !ok {
		t.Fatal("UIM not found in map")
	}
	if _, ok := m[ServiceNAS]; !ok {
		t.Fatal("NAS not found in map")
	}

	// 不应该找到的
	if _, ok := m[ServiceVOICE]; ok {
		t.Fatal("VOICE should not be in map")
	}
}

func TestServiceVersionMapEmpty(t *testing.T) {
	m := ServiceVersionMap(nil)
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestHasServiceBeforeQuery(t *testing.T) {
	c := &Client{}
	// 未查询前，乐观返回 true
	if !c.HasService(ServiceVOICE) {
		t.Fatal("HasService should return true before version query")
	}
	if !c.HasService(ServiceWDS) {
		t.Fatal("HasService should return true before version query")
	}
}

func TestHasServiceAfterQuery(t *testing.T) {
	c := &Client{
		serviceVersions: map[uint8]ServiceVersion{
			ServiceWDS: {ServiceType: ServiceWDS, Major: 1, Minor: 30},
			ServiceNAS: {ServiceType: ServiceNAS, Major: 1, Minor: 20},
		},
		versionQueried: true,
	}

	if !c.HasService(ServiceWDS) {
		t.Fatal("HasService(WDS) should return true")
	}
	if !c.HasService(ServiceNAS) {
		t.Fatal("HasService(NAS) should return true")
	}
	if c.HasService(ServiceVOICE) {
		t.Fatal("HasService(VOICE) should return false when not in version list")
	}
}

func TestGetCachedServiceVersionsBeforeQuery(t *testing.T) {
	c := &Client{}
	result := c.GetCachedServiceVersions()
	if result != nil {
		t.Fatalf("expected nil before query, got %v", result)
	}
}

func TestGetCachedServiceVersionsAfterQuery(t *testing.T) {
	original := map[uint8]ServiceVersion{
		ServiceWDS: {ServiceType: ServiceWDS, Major: 1, Minor: 30},
	}
	c := &Client{
		serviceVersions: original,
		versionQueried:  true,
	}
	result := c.GetCachedServiceVersions()
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if _, ok := result[ServiceWDS]; !ok {
		t.Fatal("WDS not found in cached versions")
	}

	// 验证返回的是拷贝而非原始引用
	result[ServiceNAS] = ServiceVersion{ServiceType: ServiceNAS}
	if len(c.serviceVersions) != 1 {
		t.Fatal("modifying returned map should not affect original")
	}
}
