import CoreImage
import CoreImage.CIFilterBuiltins
import SwiftUI

struct QRCodeView: View {
  let value: String

  var body: some View {
    Group {
      if let image = QRCodeRenderer.image(for: value) {
        Image(decorative: image, scale: 1)
          .resizable()
          .interpolation(.none)
          .scaledToFit()
      } else {
        ContentUnavailableView("QR code unavailable", systemImage: "qrcode")
      }
    }
    .accessibilityLabel("Device link QR code")
  }
}

private enum QRCodeRenderer {
  private static let context = CIContext(options: [.useSoftwareRenderer: false])

  static func image(for value: String) -> CGImage? {
    let filter = CIFilter.qrCodeGenerator()
    filter.message = Data(value.utf8)
    filter.correctionLevel = "M"
    guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 12, y: 12))
    else {
      return nil
    }
    return context.createCGImage(output, from: output.extent)
  }
}
