# Generated by the protocol buffer compiler.  DO NOT EDIT!
# source: fw.proto

import sys
_b=sys.version_info[0]<3 and (lambda x:x) or (lambda x:x.encode('latin1'))
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from google.protobuf import reflection as _reflection
from google.protobuf import symbol_database as _symbol_database
from google.protobuf import descriptor_pb2
# @@protoc_insertion_point(imports)

_sym_db = _symbol_database.Default()




DESCRIPTOR = _descriptor.FileDescriptor(
  name='fw.proto',
  package='',
  syntax='proto3',
  serialized_pb=_b('\n\x08\x66w.proto\"\'\n\x08\x41\x43\x45Match\x12\x0c\n\x04type\x18\x01 \x01(\t\x12\r\n\x05value\x18\x02 \x01(\t\"\x84\x01\n\tACEAction\x12\x0c\n\x04\x64rop\x18\x01 \x01(\x08\x12\r\n\x05limit\x18\x02 \x01(\x08\x12\x11\n\tlimitrate\x18\x03 \x01(\r\x12\x11\n\tlimitunit\x18\x04 \x01(\t\x12\x12\n\nlimitburst\x18\x05 \x01(\r\x12\x0f\n\x07portmap\x18\x06 \x01(\x08\x12\x0f\n\x07\x61ppPort\x18\x07 \x01(\r\">\n\x03\x41\x43\x45\x12\x1a\n\x07matches\x18\x01 \x03(\x0b\x32\t.ACEMatch\x12\x1b\n\x07\x61\x63tions\x18\x02 \x03(\x0b\x32\n.ACEActionBG\n\x1f\x63om.zededa.cloud.uservice.protoZ$github.com/zededa/eve/sdk/go/zconfigb\x06proto3')
)
_sym_db.RegisterFileDescriptor(DESCRIPTOR)




_ACEMATCH = _descriptor.Descriptor(
  name='ACEMatch',
  full_name='ACEMatch',
  filename=None,
  file=DESCRIPTOR,
  containing_type=None,
  fields=[
    _descriptor.FieldDescriptor(
      name='type', full_name='ACEMatch.type', index=0,
      number=1, type=9, cpp_type=9, label=1,
      has_default_value=False, default_value=_b("").decode('utf-8'),
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='value', full_name='ACEMatch.value', index=1,
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
  serialized_start=12,
  serialized_end=51,
)


_ACEACTION = _descriptor.Descriptor(
  name='ACEAction',
  full_name='ACEAction',
  filename=None,
  file=DESCRIPTOR,
  containing_type=None,
  fields=[
    _descriptor.FieldDescriptor(
      name='drop', full_name='ACEAction.drop', index=0,
      number=1, type=8, cpp_type=7, label=1,
      has_default_value=False, default_value=False,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='limit', full_name='ACEAction.limit', index=1,
      number=2, type=8, cpp_type=7, label=1,
      has_default_value=False, default_value=False,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='limitrate', full_name='ACEAction.limitrate', index=2,
      number=3, type=13, cpp_type=3, label=1,
      has_default_value=False, default_value=0,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='limitunit', full_name='ACEAction.limitunit', index=3,
      number=4, type=9, cpp_type=9, label=1,
      has_default_value=False, default_value=_b("").decode('utf-8'),
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='limitburst', full_name='ACEAction.limitburst', index=4,
      number=5, type=13, cpp_type=3, label=1,
      has_default_value=False, default_value=0,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='portmap', full_name='ACEAction.portmap', index=5,
      number=6, type=8, cpp_type=7, label=1,
      has_default_value=False, default_value=False,
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='appPort', full_name='ACEAction.appPort', index=6,
      number=7, type=13, cpp_type=3, label=1,
      has_default_value=False, default_value=0,
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
  serialized_end=186,
)


_ACE = _descriptor.Descriptor(
  name='ACE',
  full_name='ACE',
  filename=None,
  file=DESCRIPTOR,
  containing_type=None,
  fields=[
    _descriptor.FieldDescriptor(
      name='matches', full_name='ACE.matches', index=0,
      number=1, type=11, cpp_type=10, label=3,
      has_default_value=False, default_value=[],
      message_type=None, enum_type=None, containing_type=None,
      is_extension=False, extension_scope=None,
      options=None),
    _descriptor.FieldDescriptor(
      name='actions', full_name='ACE.actions', index=1,
      number=2, type=11, cpp_type=10, label=3,
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
  serialized_start=188,
  serialized_end=250,
)

_ACE.fields_by_name['matches'].message_type = _ACEMATCH
_ACE.fields_by_name['actions'].message_type = _ACEACTION
DESCRIPTOR.message_types_by_name['ACEMatch'] = _ACEMATCH
DESCRIPTOR.message_types_by_name['ACEAction'] = _ACEACTION
DESCRIPTOR.message_types_by_name['ACE'] = _ACE

ACEMatch = _reflection.GeneratedProtocolMessageType('ACEMatch', (_message.Message,), dict(
  DESCRIPTOR = _ACEMATCH,
  __module__ = 'fw_pb2'
  # @@protoc_insertion_point(class_scope:ACEMatch)
  ))
_sym_db.RegisterMessage(ACEMatch)

ACEAction = _reflection.GeneratedProtocolMessageType('ACEAction', (_message.Message,), dict(
  DESCRIPTOR = _ACEACTION,
  __module__ = 'fw_pb2'
  # @@protoc_insertion_point(class_scope:ACEAction)
  ))
_sym_db.RegisterMessage(ACEAction)

ACE = _reflection.GeneratedProtocolMessageType('ACE', (_message.Message,), dict(
  DESCRIPTOR = _ACE,
  __module__ = 'fw_pb2'
  # @@protoc_insertion_point(class_scope:ACE)
  ))
_sym_db.RegisterMessage(ACE)


DESCRIPTOR.has_options = True
DESCRIPTOR._options = _descriptor._ParseOptions(descriptor_pb2.FileOptions(), _b('\n\037com.zededa.cloud.uservice.protoZ$github.com/zededa/eve/sdk/go/zconfig'))
# @@protoc_insertion_point(module_scope)
