import sys

def process(file):
    with open(file, 'r', encoding='utf-8') as f:
        content = f.read()

    # Struct fields
    content = content.replace('\tCodecID       media.CodecID', '\tcodecID       media.CodecID')
    content = content.replace('\tByteOrder     binary.ByteOrder', '\tbyteOrder     binary.ByteOrder')
    content = content.replace('\tADPCM         params.ADPCM', '\tadpcm         params.ADPCM')
    
    content = content.replace('\tCodecID   media.CodecID', '\tcodecID   media.CodecID')
    content = content.replace('\tByteOrder binary.ByteOrder', '\tbyteOrder binary.ByteOrder')
    content = content.replace('\tADPCM     params.ADPCM', '\tadpcm     params.ADPCM')
    
    # Defaults
    content = content.replace('CodecID:       media.CodecLPCM', 'codecID:       media.CodecLPCM')
    content = content.replace('ByteOrder:     binary.LittleEndian', 'byteOrder:     binary.LittleEndian')
    content = content.replace('CodecID:   media.CodecLPCM', 'codecID:   media.CodecLPCM')
    content = content.replace('ByteOrder: binary.LittleEndian', 'byteOrder: binary.LittleEndian')
    
    # cfg usages
    content = content.replace('cfg.CodecID', 'cfg.codecID')
    content = content.replace('cfg.ByteOrder', 'cfg.byteOrder')
    content = content.replace('cfg.ADPCM', 'cfg.adpcm')
    
    # d.config / e.config usages
    content = content.replace('d.config.CodecID', 'd.config.codecID')
    content = content.replace('d.config.ByteOrder', 'd.config.byteOrder')
    content = content.replace('d.config.ADPCM', 'd.config.adpcm')
    content = content.replace('e.config.CodecID', 'e.config.codecID')
    content = content.replace('e.config.ByteOrder', 'e.config.byteOrder')
    content = content.replace('e.config.ADPCM', 'e.config.adpcm')

    with open(file, 'w', encoding='utf-8') as f:
        f.write(content)

process('internal/decoder.go')
process('internal/encoder.go')
