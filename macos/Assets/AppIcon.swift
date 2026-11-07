import Foundation
import CoreGraphics
import ImageIO
import UniformTypeIdentifiers

func color(_ red: CGFloat, _ green: CGFloat, _ blue: CGFloat, _ alpha: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: red, green: green, blue: blue, alpha: alpha)
}

func renderIcon(size: Int) -> CGImage {
    let space = CGColorSpace(name: CGColorSpace.sRGB)!
    let context = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8,
                            bytesPerRow: size * 4, space: space,
                            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.scaleBy(x: CGFloat(size) / 1024, y: CGFloat(size) / 1024)
    let plate = CGPath(roundedRect: CGRect(x: 80, y: 80, width: 864, height: 864),
                       cornerWidth: 194, cornerHeight: 194, transform: nil)
    context.saveGState()
    context.setShadow(offset: CGSize(width: 0, height: -10), blur: 22, color: color(0, 0, 0, 0.28))
    context.addPath(plate)
    context.setFillColor(color(0.09, 0.12, 0.22))
    context.fillPath()
    context.restoreGState()
    context.saveGState()
    context.addPath(plate)
    context.clip()
    let gradient = CGGradient(colorsSpace: space,
                              colors: [color(0.29, 0.34, 0.55), color(0.07, 0.10, 0.19)] as CFArray,
                              locations: [0, 1])!
    context.drawLinearGradient(gradient, start: CGPoint(x: 350, y: 944), end: CGPoint(x: 650, y: 80),
                               options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
    context.restoreGState()
    context.addPath(plate)
    context.setStrokeColor(color(1, 1, 1, 0.18))
    context.setLineWidth(3)
    context.strokePath()

    let endAngle = CGFloat.pi * 235 / 180
    context.addArc(center: CGPoint(x: 512, y: 512), radius: 300,
                   startAngle: -.pi * 55 / 180, endAngle: endAngle, clockwise: false)
    context.setStrokeColor(color(0.66, 0.72, 1, 0.85))
    context.setLineWidth(26)
    context.setLineCap(.round)
    context.strokePath()

    let star = CGMutablePath()
    star.move(to: CGPoint(x: 512, y: 760))
    star.addCurve(to: CGPoint(x: 760, y: 512), control1: CGPoint(x: 550, y: 574), control2: CGPoint(x: 574, y: 550))
    star.addCurve(to: CGPoint(x: 512, y: 264), control1: CGPoint(x: 574, y: 474), control2: CGPoint(x: 550, y: 450))
    star.addCurve(to: CGPoint(x: 264, y: 512), control1: CGPoint(x: 474, y: 450), control2: CGPoint(x: 450, y: 474))
    star.addCurve(to: CGPoint(x: 512, y: 760), control1: CGPoint(x: 450, y: 550), control2: CGPoint(x: 474, y: 574))
    star.closeSubpath()
    context.saveGState()
    context.setShadow(offset: CGSize(width: 0, height: -4), blur: 12, color: color(0, 0, 0, 0.25))
    context.addPath(star)
    context.setFillColor(color(1, 0.96, 0.86))
    context.fillPath()
    context.restoreGState()

    let dot = CGPoint(x: 512 + 300 * cos(endAngle), y: 512 + 300 * sin(endAngle))
    context.setFillColor(color(0.98, 0.67, 0.51))
    context.fillEllipse(in: CGRect(x: dot.x - 34, y: dot.y - 34, width: 68, height: 68))
    return context.makeImage()!
}

guard CommandLine.arguments.count == 2 else {
    FileHandle.standardError.write(Data("Usage: swift AppIcon.swift output.iconset\n".utf8))
    exit(1)
}
let output = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
for (name, size) in [
    ("icon_16x16.png", 16), ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32), ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128), ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256), ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512), ("icon_512x512@2x.png", 1024),
] {
    let destination = CGImageDestinationCreateWithURL(output.appendingPathComponent(name) as CFURL,
                                                     UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(destination, renderIcon(size: size), nil)
    guard CGImageDestinationFinalize(destination) else { throw CocoaError(.fileWriteUnknown) }
}
