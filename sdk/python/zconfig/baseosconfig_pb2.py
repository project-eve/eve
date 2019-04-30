# Generated by the protocol buffer compiler.  DO NOT EDIT!
# source: baseosconfig.proto

import sys
_b=sys.version_info[0]<3 and (lambda x:x) or (lambda x:x.encode('latin1'))
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from google.protobuf import reflection as _reflection
from google.protobuf import symbol_database as _symbol_database
from google.protobuf import descriptor_pb2
# @@protoc_insertion_point(imports)

_sym_db = _symbol_database.Default()


import devcommon_pb2 as devcommon__pb2
import storage_pb2 as storage__pb2


DESCRIPTOR = _descriptor.FileDescriptor(
  name='baseosconfig.proto',
  package='',
  syntax='proto3',
  serialized_pb=_b('\n\x12\x62\x61seosconfig.proto\x1a\x0f\x64\x65vcommon.proto\x1a\rstorage.proto\"1\n\tOSKeyTags\x12\x10\n\x08OSVerKey\x18\x01 \x01(\t\x12\x12\n\nOSVerValue\x18\x02 \x01(\t\"0\n\x0cOSVerDetails\x12 \n\x0c\x62\x61seOSParams\x18\x0c \x03(\x0b\x32\n.OSKeyTags\"\x9e\x01\n\x0c\x42\x61seOSConfig\x12\'\n\x0euuidandversion\x18\x01 \x01(\x0b\x32\x0f.UUIDandVersion\x12\x16\n\x06\x64rives\x18\x03 \x03(\x0b\x32\x06.Drive\x12\x10\n\x08\x61\x63tivate\x18\x04 \x01(\x08\x12\x15\n\rbaseOSVersion\x18\n \x01(\t\x12$\n\rbaseOSDetails\x18\x0b \x01(\x0b\x32\r.OSVerDetailsBG\n\x1f\x63om.zededa.cloud.uservice.protoZ$github.com/zededa/eve/sdk/go/zconfigb\x06proto3')
  ,
  dependencies=[devcommon__pb2.DESCRIPTOR,storage__pb2.DESCRIPTOR,])
_sym_db.RegisterFileDescriptor(DESCRIPTOR)




_OSKEYTAGS = _descriptor.Descriptor(
  name='OSKeyTags',
  full_name='OSKeyTags',
  filename=None,
  file=DESCRIPTOR,
  containing_type=None,
  fields=[
    _descriptor.FieldDescriptor(
      name='OSVerKey', full_name='OSKeyTags.OSVerKey', index=0,
      number=1, type=9, cpp_type=9, label=1,
      has_default_value=False, default_value=_b("").decode('utf-8'),
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='OSVerValue', full_name='OSKeyTags.OSVerValue', index=1,
      number=2, type=9, cpp_type=9, label=1,
      has_default_value=False, default_value=_b("").decode('utf-8'),
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
  ],
  extensions=[
  ],
  nested_types=[],
  enum_types=[
  ],
  options=None,
  is_extendable=False,
  syntax='proto3',
  extension_ranges=[],
  oneofs=[
  ],
  serialized_start=54,
  serialized_end=103,
)


_OSVERDETAILS = _descriptor.Descriptor(
  name='OSVerDetails',
  full_name='OSVerDetails',
  filename=None,
  file=DESCRIPTOR,
  containing_type=None,
  fields=[
    _descriptor.FieldDescriptor(
      name='baseOSParams', full_name='OSVerDetails.baseOSParams', index=0,
      number=12, type=11, cpp_type=10, label=3,
      has_default_value=False, default_value=[],
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
  ],
  extensions=[
  ],
  nested_types=[],
  enum_types=[
  ],
  options=None,
  is_extendable=False,
  syntax='proto3',
  extension_ranges=[],
  oneofs=[
  ],
  serialized_start=105,
  serialized_end=153,
)


_BASEOSCONFIG = _descriptor.Descriptor(
  name='BaseOSConfig',
  full_name='BaseOSConfig',
  filename=None,
  file=DESCRIPTOR,
  containing_type=None,
  fields=[
    _descriptor.FieldDescriptor(
      name='uuidandversion', full_name='BaseOSConfig.uuidandversion', index=0,
      number=1, type=11, cpp_type=10, label=1,
      has_default_value=False, default_value=None,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='drives', full_name='BaseOSConfig.drives', index=1,
      number=3, type=11, cpp_type=10, label=3,
      has_default_value=False, default_value=[],
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='activate', full_name='BaseOSConfig.activate', index=2,
      number=4, type=8, cpp_type=7, label=1,
      has_default_value=False, default_value=False,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='baseOSVersion', full_name='BaseOSConfig.baseOSVersion', index=3,
      number=10, type=9, cpp_type=9, label=1,
      has_default_value=False, default_value=_b("").decode('utf-8'),
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='baseOSDetails', full_name='BaseOSConfig.baseOSDetails', index=4,
      number=11, type=11, cpp_type=10, label=1,
      has_default_value=False, default_value=None,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
  ],
  extensions=[
  ],
  nested_types=[],
  enum_types=[
  ],
  options=None,
  is_extendable=False,
  syntax='proto3',
  extension_ranges=[],
  oneofs=[
  ],
  serialized_start=156,
  serialized_end=314,
)

_OSVERDETAILS.fields_by_name['baseOSParams'].message_type = _OSKEYTAGS
_BASEOSCONFIG.fields_by_name['uuidandversion'].message_type = devcommon__pb2._UUIDANDVERSION
_BASEOSCONFIG.fields_by_name['drives'].message_type = storage__pb2._DRIVE
_BASEOSCONFIG.fields_by_name['baseOSDetails'].message_type = _OSVERDETAILS
DESCRIPTOR.message_types_by_name['OSKeyTags'] = _OSKEYTAGS
DESCRIPTOR.message_types_by_name['OSVerDetails'] = _OSVERDETAILS
DESCRIPTOR.message_types_by_name['BaseOSConfig'] = _BASEOSCONFIG

OSKeyTags = _reflection.GeneratedProtocolMessageType('OSKeyTags', (_message.Message,), dict(
  DESCRIPTOR = _OSKEYTAGS,
  __module__ = 'baseosconfig_pb2'
  # @@protoc_insertion_point(class_scope:OSKeyTags)
  ))
_sym_db.RegisterMessage(OSKeyTags)

OSVerDetails = _reflection.GeneratedProtocolMessageType('OSVerDetails', (_message.Message,), dict(
  DESCRIPTOR = _OSVERDETAILS,
  __module__ = 'baseosconfig_pb2'
  # @@protoc_insertion_point(class_scope:OSVerDetails)
  ))
_sym_db.RegisterMessage(OSVerDetails)

BaseOSConfig = _reflection.GeneratedProtocolMessageType('BaseOSConfig', (_message.Message,), dict(
  DESCRIPTOR = _BASEOSCONFIG,
  __module__ = 'baseosconfig_pb2'
  # @@protoc_insertion_point(class_scope:BaseOSConfig)
  ))
_sym_db.RegisterMessage(BaseOSConfig)


DESCRIPTOR.has_options = True
DESCRIPTOR._options = _descriptor._ParseOptions(descriptor_pb2.FileOptions(), _b('\n\037com.zededa.cloud.uservice.protoZ$github.com/zededa/eve/sdk/go/zconfig'))
# @@protoc_insertion_point(module_scope)
