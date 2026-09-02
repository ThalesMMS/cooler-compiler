class Base {};
class Middle inherits Base {};
class Deep inherits Middle {};
class Main inherits IO {
  subject() : Base {{ out_string("S"); new Deep; }};
  main() : Object {
    case subject() of
      base : Base => out_string("Base");
      middle : Middle => out_string(middle.type_name());
    esac
  };
};
