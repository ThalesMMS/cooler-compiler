class Base {
  identity() : SELF_TYPE { self };
  chain() : SELF_TYPE { identity().identity() };
};

class Child inherits Base {};

class Main inherits IO {
  label : String <- "attribute";
  choose(label : String) : String {
    let label : String <- label.concat("/let") in label
  };
  maybe(flag : Bool) : Object {
    if flag then new Object else let missing : Object in missing fi
  };
  main() : Object {{
    out_string(choose("formal")); out_string("/");
    case new Child of
      label : Base => out_string(label.chain().type_name());
    esac;
    out_string("/");
    if isvoid maybe(false) then out_string("T") else out_string("F") fi;
  }};
};
