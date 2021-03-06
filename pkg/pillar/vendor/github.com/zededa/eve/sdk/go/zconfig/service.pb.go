// Code generated by protoc-gen-go. DO NOT EDIT.
// source: service.proto

package zconfig

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

type ZSrvType int32

const (
	ZSrvType_ZsrvFirst      ZSrvType = 0
	ZSrvType_ZsrvStrongSwan ZSrvType = 1
	ZSrvType_ZsrvLISP       ZSrvType = 2
	ZSrvType_ZsrvBridge     ZSrvType = 3
	ZSrvType_ZsrvNAT        ZSrvType = 4
	ZSrvType_ZsrvLB         ZSrvType = 5
	ZSrvType_ZsrvLast       ZSrvType = 255
)

var ZSrvType_name = map[int32]string{
	0:   "ZsrvFirst",
	1:   "ZsrvStrongSwan",
	2:   "ZsrvLISP",
	3:   "ZsrvBridge",
	4:   "ZsrvNAT",
	5:   "ZsrvLB",
	255: "ZsrvLast",
}

var ZSrvType_value = map[string]int32{
	"ZsrvFirst":      0,
	"ZsrvStrongSwan": 1,
	"ZsrvLISP":       2,
	"ZsrvBridge":     3,
	"ZsrvNAT":        4,
	"ZsrvLB":         5,
	"ZsrvLast":       255,
}

func (x ZSrvType) String() string {
	return proto.EnumName(ZSrvType_name, int32(x))
}

func (ZSrvType) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_a0b84a42fa06f626, []int{0}
}

// Service Opaque config. In future we might add more fields here
// but idea is here. This is service specific configuration.
type ServiceOpaqueConfig struct {
	Oconfig              string   `protobuf:"bytes,1,opt,name=oconfig,proto3" json:"oconfig,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ServiceOpaqueConfig) Reset()         { *m = ServiceOpaqueConfig{} }
func (m *ServiceOpaqueConfig) String() string { return proto.CompactTextString(m) }
func (*ServiceOpaqueConfig) ProtoMessage()    {}
func (*ServiceOpaqueConfig) Descriptor() ([]byte, []int) {
	return fileDescriptor_a0b84a42fa06f626, []int{0}
}

func (m *ServiceOpaqueConfig) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ServiceOpaqueConfig.Unmarshal(m, b)
}
func (m *ServiceOpaqueConfig) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ServiceOpaqueConfig.Marshal(b, m, deterministic)
}
func (m *ServiceOpaqueConfig) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ServiceOpaqueConfig.Merge(m, src)
}
func (m *ServiceOpaqueConfig) XXX_Size() int {
	return xxx_messageInfo_ServiceOpaqueConfig.Size(m)
}
func (m *ServiceOpaqueConfig) XXX_DiscardUnknown() {
	xxx_messageInfo_ServiceOpaqueConfig.DiscardUnknown(m)
}

var xxx_messageInfo_ServiceOpaqueConfig proto.InternalMessageInfo

func (m *ServiceOpaqueConfig) GetOconfig() string {
	if m != nil {
		return m.Oconfig
	}
	return ""
}

// Service Lisp config
type ServiceLispConfig struct {
	LispMSs              []*ZcServicePoint `protobuf:"bytes,1,rep,name=LispMSs,proto3" json:"LispMSs,omitempty"`
	LispInstanceId       uint32            `protobuf:"varint,2,opt,name=LispInstanceId,proto3" json:"LispInstanceId,omitempty"`
	Allocate             bool              `protobuf:"varint,3,opt,name=allocate,proto3" json:"allocate,omitempty"`
	Exportprivate        bool              `protobuf:"varint,4,opt,name=exportprivate,proto3" json:"exportprivate,omitempty"`
	Allocationprefix     []byte            `protobuf:"bytes,5,opt,name=allocationprefix,proto3" json:"allocationprefix,omitempty"`
	Allocationprefixlen  uint32            `protobuf:"varint,6,opt,name=allocationprefixlen,proto3" json:"allocationprefixlen,omitempty"`
	Experimental         bool              `protobuf:"varint,20,opt,name=Experimental,proto3" json:"Experimental,omitempty"`
	XXX_NoUnkeyedLiteral struct{}          `json:"-"`
	XXX_unrecognized     []byte            `json:"-"`
	XXX_sizecache        int32             `json:"-"`
}

func (m *ServiceLispConfig) Reset()         { *m = ServiceLispConfig{} }
func (m *ServiceLispConfig) String() string { return proto.CompactTextString(m) }
func (*ServiceLispConfig) ProtoMessage()    {}
func (*ServiceLispConfig) Descriptor() ([]byte, []int) {
	return fileDescriptor_a0b84a42fa06f626, []int{1}
}

func (m *ServiceLispConfig) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ServiceLispConfig.Unmarshal(m, b)
}
func (m *ServiceLispConfig) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ServiceLispConfig.Marshal(b, m, deterministic)
}
func (m *ServiceLispConfig) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ServiceLispConfig.Merge(m, src)
}
func (m *ServiceLispConfig) XXX_Size() int {
	return xxx_messageInfo_ServiceLispConfig.Size(m)
}
func (m *ServiceLispConfig) XXX_DiscardUnknown() {
	xxx_messageInfo_ServiceLispConfig.DiscardUnknown(m)
}

var xxx_messageInfo_ServiceLispConfig proto.InternalMessageInfo

func (m *ServiceLispConfig) GetLispMSs() []*ZcServicePoint {
	if m != nil {
		return m.LispMSs
	}
	return nil
}

func (m *ServiceLispConfig) GetLispInstanceId() uint32 {
	if m != nil {
		return m.LispInstanceId
	}
	return 0
}

func (m *ServiceLispConfig) GetAllocate() bool {
	if m != nil {
		return m.Allocate
	}
	return false
}

func (m *ServiceLispConfig) GetExportprivate() bool {
	if m != nil {
		return m.Exportprivate
	}
	return false
}

func (m *ServiceLispConfig) GetAllocationprefix() []byte {
	if m != nil {
		return m.Allocationprefix
	}
	return nil
}

func (m *ServiceLispConfig) GetAllocationprefixlen() uint32 {
	if m != nil {
		return m.Allocationprefixlen
	}
	return 0
}

func (m *ServiceLispConfig) GetExperimental() bool {
	if m != nil {
		return m.Experimental
	}
	return false
}

type ServiceInstanceConfig struct {
	Id          string   `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Displayname string   `protobuf:"bytes,2,opt,name=displayname,proto3" json:"displayname,omitempty"`
	Srvtype     ZSrvType `protobuf:"varint,3,opt,name=srvtype,proto3,enum=ZSrvType" json:"srvtype,omitempty"`
	// Optional in future we might need this
	//	VmConfig fixedresources = 3;
	//	repeated Drive drives = 4;
	Activate bool `protobuf:"varint,5,opt,name=activate,proto3" json:"activate,omitempty"`
	// towards application which networkUUID to use
	// FIXME: In future there might multiple application network assignment
	// so this will become repeated.
	Applink string `protobuf:"bytes,10,opt,name=applink,proto3" json:"applink,omitempty"`
	// towards devices which phyiscal or logical adapter to use
	Devlink *Adapter `protobuf:"bytes,20,opt,name=devlink,proto3" json:"devlink,omitempty"`
	// Opaque configuration
	Cfg                  *ServiceOpaqueConfig `protobuf:"bytes,30,opt,name=cfg,proto3" json:"cfg,omitempty"`
	LispCfg              *ServiceLispConfig   `protobuf:"bytes,31,opt,name=lispCfg,proto3" json:"lispCfg,omitempty"`
	XXX_NoUnkeyedLiteral struct{}             `json:"-"`
	XXX_unrecognized     []byte               `json:"-"`
	XXX_sizecache        int32                `json:"-"`
}

func (m *ServiceInstanceConfig) Reset()         { *m = ServiceInstanceConfig{} }
func (m *ServiceInstanceConfig) String() string { return proto.CompactTextString(m) }
func (*ServiceInstanceConfig) ProtoMessage()    {}
func (*ServiceInstanceConfig) Descriptor() ([]byte, []int) {
	return fileDescriptor_a0b84a42fa06f626, []int{2}
}

func (m *ServiceInstanceConfig) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ServiceInstanceConfig.Unmarshal(m, b)
}
func (m *ServiceInstanceConfig) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ServiceInstanceConfig.Marshal(b, m, deterministic)
}
func (m *ServiceInstanceConfig) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ServiceInstanceConfig.Merge(m, src)
}
func (m *ServiceInstanceConfig) XXX_Size() int {
	return xxx_messageInfo_ServiceInstanceConfig.Size(m)
}
func (m *ServiceInstanceConfig) XXX_DiscardUnknown() {
	xxx_messageInfo_ServiceInstanceConfig.DiscardUnknown(m)
}

var xxx_messageInfo_ServiceInstanceConfig proto.InternalMessageInfo

func (m *ServiceInstanceConfig) GetId() string {
	if m != nil {
		return m.Id
	}
	return ""
}

func (m *ServiceInstanceConfig) GetDisplayname() string {
	if m != nil {
		return m.Displayname
	}
	return ""
}

func (m *ServiceInstanceConfig) GetSrvtype() ZSrvType {
	if m != nil {
		return m.Srvtype
	}
	return ZSrvType_ZsrvFirst
}

func (m *ServiceInstanceConfig) GetActivate() bool {
	if m != nil {
		return m.Activate
	}
	return false
}

func (m *ServiceInstanceConfig) GetApplink() string {
	if m != nil {
		return m.Applink
	}
	return ""
}

func (m *ServiceInstanceConfig) GetDevlink() *Adapter {
	if m != nil {
		return m.Devlink
	}
	return nil
}

func (m *ServiceInstanceConfig) GetCfg() *ServiceOpaqueConfig {
	if m != nil {
		return m.Cfg
	}
	return nil
}

func (m *ServiceInstanceConfig) GetLispCfg() *ServiceLispConfig {
	if m != nil {
		return m.LispCfg
	}
	return nil
}

func init() {
	proto.RegisterEnum("ZSrvType", ZSrvType_name, ZSrvType_value)
	proto.RegisterType((*ServiceOpaqueConfig)(nil), "ServiceOpaqueConfig")
	proto.RegisterType((*ServiceLispConfig)(nil), "ServiceLispConfig")
	proto.RegisterType((*ServiceInstanceConfig)(nil), "ServiceInstanceConfig")
}

func init() { proto.RegisterFile("service.proto", fileDescriptor_a0b84a42fa06f626) }

var fileDescriptor_a0b84a42fa06f626 = []byte{
	// 514 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x6c, 0x93, 0xdf, 0x6e, 0xd3, 0x30,
	0x18, 0xc5, 0x49, 0xba, 0x36, 0xed, 0xd7, 0x3f, 0x0b, 0xde, 0x90, 0xa2, 0x5d, 0xb0, 0xa8, 0x4c,
	0x53, 0x99, 0x50, 0x82, 0xca, 0x13, 0xac, 0x08, 0x50, 0xa5, 0x01, 0x53, 0xb2, 0xab, 0xde, 0x79,
	0xb1, 0x1b, 0xac, 0x25, 0xb6, 0x71, 0xdc, 0xd0, 0xee, 0x61, 0x78, 0x00, 0x5e, 0x12, 0x94, 0xc4,
	0x41, 0xeb, 0xb6, 0x3b, 0x9f, 0x73, 0x7e, 0x96, 0xec, 0xe3, 0xcf, 0x30, 0x2e, 0xa8, 0x2a, 0x59,
	0x42, 0x03, 0xa9, 0x84, 0x16, 0x27, 0x87, 0x84, 0x96, 0x89, 0xc8, 0x73, 0xc1, 0x1b, 0x63, 0x1a,
	0xc2, 0x51, 0xdc, 0x10, 0xdf, 0x25, 0xfe, 0xb9, 0xa1, 0x1f, 0x05, 0x5f, 0xb3, 0x14, 0x79, 0xe0,
	0x88, 0xa4, 0x5e, 0x7a, 0x96, 0x6f, 0xcd, 0x06, 0x51, 0x2b, 0xa7, 0x7f, 0x6c, 0x78, 0x69, 0x76,
	0x5c, 0xb1, 0x42, 0x1a, 0xfe, 0x2d, 0x38, 0x95, 0xfa, 0x1a, 0x17, 0x9e, 0xe5, 0x77, 0x66, 0xc3,
	0xf9, 0x61, 0xb0, 0x4a, 0x0c, 0x76, 0x2d, 0x18, 0xd7, 0x51, 0x9b, 0xa3, 0x73, 0x98, 0x54, 0xcb,
	0x25, 0x2f, 0x34, 0xe6, 0x09, 0x5d, 0x12, 0xcf, 0xf6, 0xad, 0xd9, 0x38, 0x7a, 0xe4, 0xa2, 0x13,
	0xe8, 0xe3, 0x2c, 0x13, 0x09, 0xd6, 0xd4, 0xeb, 0xf8, 0xd6, 0xac, 0x1f, 0xfd, 0xd7, 0xe8, 0x0c,
	0xc6, 0x74, 0x2b, 0x85, 0xd2, 0x52, 0xb1, 0xb2, 0x02, 0x0e, 0x6a, 0x60, 0xdf, 0x44, 0x17, 0xe0,
	0x9a, 0x1d, 0x4c, 0x70, 0xa9, 0xe8, 0x9a, 0x6d, 0xbd, 0xae, 0x6f, 0xcd, 0x46, 0xd1, 0x13, 0x1f,
	0xbd, 0x87, 0xa3, 0xc7, 0x5e, 0x46, 0xb9, 0xd7, 0xab, 0x8f, 0xf6, 0x5c, 0x84, 0xa6, 0x30, 0xfa,
	0xb4, 0x95, 0x54, 0xb1, 0x9c, 0x72, 0x8d, 0x33, 0xef, 0xb8, 0x3e, 0xc2, 0x9e, 0x37, 0xfd, 0x6d,
	0xc3, 0x2b, 0xd3, 0x42, 0x7b, 0x33, 0x53, 0xd8, 0x04, 0x6c, 0x46, 0x4c, 0xb7, 0x36, 0x23, 0xc8,
	0x87, 0x21, 0x61, 0x85, 0xcc, 0xf0, 0x8e, 0xe3, 0x9c, 0xd6, 0x95, 0x0c, 0xa2, 0x87, 0x16, 0x7a,
	0x03, 0x4e, 0xa1, 0x4a, 0xbd, 0x93, 0x4d, 0x1d, 0x93, 0xf9, 0x20, 0x58, 0xc5, 0xaa, 0xbc, 0xd9,
	0x49, 0x1a, 0xb5, 0x49, 0x5d, 0x5a, 0xa2, 0x9b, 0x4e, 0xba, 0xa6, 0x34, 0xa3, 0xab, 0x37, 0xc5,
	0x52, 0x66, 0x8c, 0xdf, 0x79, 0xd0, 0xbc, 0xa9, 0x91, 0x68, 0x0a, 0x0e, 0xa1, 0x65, 0x9d, 0x54,
	0xb7, 0x18, 0xce, 0xfb, 0xc1, 0x25, 0xc1, 0x52, 0x53, 0x15, 0xb5, 0x01, 0x3a, 0x87, 0x4e, 0xb2,
	0x4e, 0xbd, 0xd7, 0x75, 0x7e, 0x1c, 0x3c, 0x33, 0x34, 0x51, 0x05, 0xa0, 0x77, 0xe0, 0x64, 0xd5,
	0x5c, 0xac, 0x53, 0xef, 0xb4, 0x66, 0x51, 0xf0, 0x64, 0x5c, 0xa2, 0x16, 0xb9, 0x28, 0xa0, 0xdf,
	0x5e, 0x02, 0x8d, 0x61, 0xb0, 0x2a, 0x54, 0xf9, 0x99, 0xa9, 0x42, 0xbb, 0x2f, 0x10, 0x82, 0x49,
	0x25, 0x63, 0xad, 0x04, 0x4f, 0xe3, 0x5f, 0x98, 0xbb, 0x16, 0x1a, 0x41, 0xbf, 0xf2, 0xae, 0x96,
	0xf1, 0xb5, 0x6b, 0xa3, 0x09, 0x40, 0xa5, 0x16, 0x8a, 0x91, 0x94, 0xba, 0x1d, 0x34, 0x04, 0xa7,
	0xd2, 0xdf, 0x2e, 0x6f, 0xdc, 0x03, 0x04, 0xd0, 0xab, 0xd1, 0x85, 0xdb, 0x45, 0x63, 0xb3, 0x0d,
	0x17, 0xda, 0xfd, 0x6b, 0x2d, 0xbe, 0xc0, 0x69, 0x22, 0xf2, 0xe0, 0x9e, 0x12, 0x4a, 0x70, 0x90,
	0x64, 0x62, 0x43, 0x82, 0xcd, 0xde, 0x3f, 0x59, 0x9d, 0xa5, 0x4c, 0xff, 0xd8, 0xdc, 0x06, 0x89,
	0xc8, 0xc3, 0x86, 0x0b, 0x69, 0x49, 0xc3, 0x82, 0xdc, 0x85, 0xa9, 0x08, 0xef, 0x9b, 0xbf, 0x70,
	0xdb, 0xab, 0xe1, 0x0f, 0xff, 0x02, 0x00, 0x00, 0xff, 0xff, 0x30, 0x2d, 0xa0, 0xaf, 0x65, 0x03,
	0x00, 0x00,
}
